package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
)

//ApplicationDataPath
var ApplicationDataPath string

//WebserverDockerName is the name of nginx container
const WebserverDockerName = "ambassador_webserver"

//ConfDirectory houses the updated conf files
var ConfDirectory string
var docker *dockerclient.DockerClient

func main() {
	docker, _ = dockerclient.NewDockerClient("unix:///var/run/docker.sock", nil)

	flag.StringVar(&ApplicationDataPath, "applicationDataPath", "./applicationDataFiles", "")
	flag.StringVar(&ConfDirectory, "confDirectory", ".", "")
	flag.Parse()

	r := mux.NewRouter()
	r.HandleFunc("/ambassador/webhookchange", AppchangeHandler).Methods("POST")
	r.HandleFunc("/ambassador/manual", ManualchangeHandler).Methods("GET")
	r.HandleFunc("/ambassador/manual", ManualchangeHandler).Methods("POST")

	http.ListenAndServe(":9000", r)
}

func loadApplicationDataFiles() []ApplicationData {

	var filePath string
	var appManifest ApplicationData
	var manifests []ApplicationData

	f, e := os.Open(ApplicationDataPath)
	if e != nil {
		log.Fatalf("error opening file: %v", e)
	}
	defer f.Close()

	files, e := f.Readdir(0)
	var buffer bytes.Buffer
	for _, tmpFile := range files {
		//Reset appManifest to not leak data from other files
		//into new struct
		appManifest = ApplicationData{}
		buffer.Reset()
		if tmpFile.IsDir() == true {
			continue
		}

		if strings.Contains(tmpFile.Name(), ".json") {

			//Build filepath string
			buffer.WriteString(ApplicationDataPath)
			buffer.WriteString("/")
			buffer.WriteString(tmpFile.Name())

			// Get File Path
			filePath = buffer.String()

			// Get JSON File Contents
			byteArray, err := ioutil.ReadFile(filePath)
			if err != nil {
				panic(err)
			}

			//Unmarshal the content into a applicationdata object
			err = json.Unmarshal(byteArray, &appManifest)
			if err != nil {
				log.Print(err)
				continue
			}
			manifests = append(manifests, appManifest)
		}
	}
	return manifests
}

//AppchangeHandler will take on a payload from bit bucket and process the data
func AppchangeHandler(w http.ResponseWriter, r *http.Request) {

	//Unmarshal into Bitbucket Payload object
	var bitbucketObject BitbucketPayload
	var sApplicationData ApplicationData

	err := json.NewDecoder(r.Body).Decode(&bitbucketObject)
	if err != nil {
		log.Print(err)
	}
	log.Println("Form Body", r.Body)
	log.Println("Request Header", r.Header)
	log.Println("bitbucketObject", bitbucketObject)

	sApplicationData = findManifestByName(bitbucketObject.GetRepositoryName())
	log.Println("sApplicationData", sApplicationData)
	if &sApplicationData != nil {
		ExecutePayload(sApplicationData, bitbucketObject)
	}

}

//ManualchangeHandler allows user to build an image and run the container manually
func ManualchangeHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error manual change:", r)
		}
	}()
	var data interface{}
	r.ParseForm()
	repository := r.FormValue("repository")
	branchName := r.FormValue("branch")

	if repository != "" && branchName != "" {
		var bitbucketObject = NewBitbucketPayload()
		bitbucketObject.SetRepositoryName(repository)
		bitbucketObject.SetBranchName(branchName)
		sApplicationData := findManifestByName(bitbucketObject.GetRepositoryName())

		log.Println("ApplicationData", sApplicationData)
		log.Println("bitbucket", bitbucketObject)

		if &sApplicationData != nil {
			go ExecutePayload(sApplicationData, bitbucketObject)
		}
	}

	fmt.Fprint(w, `<h1>Manually Starting Images</h1>
			<form action="/ambassador/manual" method="POST">
			<div>
			    <h3>Repository Name</h3>
			    <input type="text" name="repository" value="">
			    <h3>Branch Name</h3>
			    <input type="text" name="branch" value="">
			    <br>
			<div><input type="submit" value="Submit"></div>
			</form>
		`, data)

}
func findManifestByName(name string) ApplicationData {
	//Parse Manifest Files for the appropriate application
	manifests := loadApplicationDataFiles()
	var sApplicationData ApplicationData
	var foundApp = false

	for _, manifest := range manifests {
		if strings.ToLower(manifest.Name) == strings.ToLower(name) {
			sApplicationData = manifest
			foundApp = true
		}
	}
	if !foundApp {
		log.Print("Could not find manifest file for ", name)
	}

	return sApplicationData
}

//TestApplication tests application
func TestApplication(sApplicationData ApplicationData, bitbucketObject BitbucketPayload) {
	if sApplicationData.HasTest == false {
		return
	}
	log.Println("Running Test", sApplicationData.DockercomposeTestCmd)
	buildCommand := exec.Command("docker-compose", sApplicationData.DockercomposeTestCmd...)
	_, err := buildCommand.CombinedOutput()
	if err != nil {
		panic(err)
	}

}

func getImageFromDockerfile(filePath string) string {
	byteArray, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Println("getImageFromFile Error: ", err)
	}

	r, err := regexp.Compile("[^FROM\\s](\\S+)")
	imageByteArray := r.Find(byteArray)
	return string(imageByteArray)
}

//ExecutePayload parses the payload and continues to processes
func ExecutePayload(sApplicationData ApplicationData, bitbucketObject BitbucketPayload) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error Executing Payload:", r)
		}
	}()

	//TODO: Find dockerfile location
	if _, err := os.Stat(sApplicationData.DockerfilePath); os.IsNotExist(err) {
		log.Printf("no such file or directory: %s", sApplicationData.DockerfilePath)
		return
	}

	//TODO:Just in case there is a dangling tmpDockerfile laying around
	removeTmpDockerfile(&sApplicationData)

	//TODO: Replace branch from git pull command in dockerfile
	//However the dockerfile is git managed and we don't want
	//to change this forever
	//We need to copy the file and update that file
	ReplaceStringInFile(&sApplicationData, bitbucketObject)

	//TODO: build image
	if buildImageViaCLI(&sApplicationData) {

		//Start docker compose application
		log.Println("Starting Docker Compose Application")
		runCommand := exec.Command("docker-compose", sApplicationData.DockercomposeRunCmd...)
		_, err := runCommand.CombinedOutput()
		if err != nil {
			panic(err)
		}

		//Push new container to registry
		log.Println("Docker push ", sApplicationData.Image)
		log.Println(sApplicationData.Image)
		pushCommand := exec.Command("docker", "push", fmt.Sprintf("%s:%s", sApplicationData.Image, strings.ToLower(bitbucketObject.GetBranchName())))
		pushOutput, err := pushCommand.CombinedOutput()
		if err != nil {
			panic(err)
		}
		log.Println(string(pushOutput))

		if sApplicationData.HasTest && sApplicationData.IsTesting == false {
			go TestApplication(sApplicationData, bitbucketObject)
		}

		if removeTmpDockerfile(&sApplicationData) == false {
			panic(fmt.Sprintf("Could not remove tmpDockerfile: %s", sApplicationData.DockerfilePath))
		}
	} else {
		fmt.Println("Could not build following image:", sApplicationData)
		fmt.Println("Payload: Repository Name", bitbucketObject.GetRepositoryName())
		fmt.Println("Payload: Branch Name", bitbucketObject.GetBranchName())
	}
}

//RemoveDeadImages remove images with <none>:<none> tags
func RemoveDeadImages() {

	images, err := docker.ListImages(true)
	if err != nil {
		fmt.Println("cannot get containers: %s", err)
	}
	imageNames := []string{}
	for _, i := range images {
		for _, r := range i.RepoTags {
			if r == "<none>:<none>" {
				fmt.Println(i)
				imageNames = append(imageNames, i.Id)
				docker.RemoveImage(i.Id, false)
			} else {
				fmt.Println(r)
			}
		}
	}
}

func buildImageViaCLI(sApplication *ApplicationData) bool {
	response := true
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error Recovered from Building Image", sApplication.Name, r)
			response = false
		}
	}()

	pathError := os.Chdir(sApplication.DockerfilePath)
	if pathError != nil {
		panic(pathError)
	}

	//Get the folder that houses the current docker-compose file
	//run the appropriate build function for this docker-compose file
	//Lets name the image this name
	baseImage := path.Base(sApplication.DockerfilePath)
	log.Println("Current Directory", sApplication.DockerfilePath)
	log.Println("Building: ", sApplication.Name)
	log.Println("Image: ", sApplication.Image)
	log.Println("docker-compose build", sApplication.DockercomposeBuildCmd)

	//Run Build Command
	buildCommand := exec.Command("docker-compose", sApplication.DockercomposeBuildCmd...)
	buildOutput, err := buildCommand.CombinedOutput()
	if err != nil {
		panic(buildOutput)
	}

	log.Println(string(buildOutput))
	//Sleep for 3 seconds so that the container can be finalized by docker
	time.Sleep(3 * time.Second)
	//TODO: after building the image who knows how
	//long it will take to Finished

	images, err := docker.ListImages(false)
	if err != nil {
		log.Println(err)
	}
forever_loop:
	for {
		for _, image := range images {
			for _, tag := range image.RepoTags {
				if tag == baseImage || tag == fmt.Sprintf("%s:latest", baseImage) {
					log.Println("Found image", tag, sApplication.Image)
					break forever_loop
				}
			}
		}
	}
	return response
}

func removeTmpDockerfile(sApplicationData *ApplicationData) bool {
	response := true
	defer func() {
		if r := recover(); r != nil {
			log.Println("Could Not Remove Tmp Docker File", r)
		}
	}()

	tmpFilepath := fmt.Sprintf("%s/tmp%s", sApplicationData.DockerfilePath, sApplicationData.Dockerfilename)
	log.Println("Attempting to remove: ", tmpFilepath)
	if _, err := os.Stat(tmpFilepath); err == nil {
		log.Println("Removing tmp file: ", tmpFilepath)
		err = os.Remove(tmpFilepath)
		if err != nil {
			panic(err)
		}
	} else {
		log.Println("tmpFilePath does not exist")
	}

	return response
}

//ReplaceStringInFile replaces string inside file
//used to replace string inside docker file
func ReplaceStringInFile(sApplicationData *ApplicationData, bitbucketObject BitbucketPayload) string {

	currentBranch := fmt.Sprintf("%s%s", "git clone -b ", bitbucketObject.GetBranchName())
	log.Println("Current Branch: ", currentBranch)

	//Read in the master docker file for this application
	filepath := fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, sApplicationData.Dockerfilename)
	//Change the dockerFileName to the tmp version
	//Later we will use this value for building
	//sApplicationData.Dockerfilename = fmt.Sprintf("tmp%s", sApplicationData.Dockerfilename)
	tmpFilePath := fmt.Sprintf("%s/tmp%s", sApplicationData.DockerfilePath, sApplicationData.Dockerfilename)
	byteArray, err := ioutil.ReadFile(filepath)

	var newString string
	if err != nil {
		panic(err)
	}

	r, e := regexp.Compile("git clone -b \\w+")
	if e != nil {
		panic(e)
	}

	newString = string(r.ReplaceAll(byteArray, []byte(currentBranch)))

	file, err := os.OpenFile(tmpFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Print("Error Opening: ", err)
	}
	defer file.Close()

	_, err = file.WriteString(newString)
	if err != nil {
		log.Print("Error Writing", err)
	}
	log.Println("Created New TmpDockerfile", tmpFilePath)
	return newString
}
func logit(jsonByteArray []byte) {
	f, err := os.OpenFile("testlogfile", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	defer f.Close()
	log.SetOutput(f)
	log.Println(string(jsonByteArray))
}
