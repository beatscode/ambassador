package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"time"

	"text/template"

	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
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
	//Set the mode of the application to test
	sApplicationData.SetTestMode(true)

	//Set the testdocker file path

	sApplicationData.DockerfilePath = sApplicationData.TestDockerfilepath

	ExecutePayload(sApplicationData, bitbucketObject)
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
	buildImageViaCLI(&sApplicationData)

	//TODO: Run Container
	ContainerInfo := runContainer(sApplicationData)

	StopOldContainers(sApplicationData, ContainerInfo)

	//TODO: Get container ip and port
	updateApplicationCurrentPort(&sApplicationData, ContainerInfo)

	//TODO: Reload web server
	// Only update the nginx conf we are not testing
	if sApplicationData.IsTesting == false {
		//TODO: update nginx conf
		UpdateApplicationNginxConf(sApplicationData)

		//stopOldContainers()
		Reloadwebserver()
	}

	if sApplicationData.HasTest && sApplicationData.IsTesting == false {
		go TestApplication(sApplicationData, bitbucketObject)
	}
}

func updateApplicationCurrentPort(sApplicationData *ApplicationData, ContainerInfo *dockerclient.ContainerInfo) {
	//Get the integer from the tcp string "80/tcp"
	r, e := regexp.Compile("\\d+")
	if e != nil {
		log.Println("Regexp", e)
	}

	exposedPortNumber := string(r.Find([]byte(sApplicationData.Exposedport)))

	for portString := range ContainerInfo.NetworkSettings.Ports {
		if portString == sApplicationData.Exposedport {
			sApplicationData.CurrentPort = exposedPortNumber
			sApplicationData.IP = ContainerInfo.NetworkSettings.IPAddress
		}
	}
}

//StopOldContainers stops old containers
func StopOldContainers(sApplicationData ApplicationData, cInfo *dockerclient.ContainerInfo) {
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		log.Fatalf("cannot get containers: %s", err)
	}

	//Only find applications with the same name
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.Contains(name, sApplicationData.Name) == true && (c.Image == sApplicationData.Image || c.Image == fmt.Sprintf("%s:latest", sApplicationData.Image)) {
				if cInfo.Name != name {
					log.Println("Killing: ", name, " App Name: ", sApplicationData.Name, " ID: ", c.Id, " IMAGE: ", c.Image)
					err = docker.KillContainer(c.Id, "SIGKILL")
					if err != nil {
						log.Println("Kill Container Error: ", err)
					}
					//Remove Killed Container
					err = docker.RemoveContainer(c.Id, true, true)
					if err != nil {
						log.Println("Removing Container Error: ", err)
					}
				}
			}
		}
	}
}
func runContainer(sApplicationData ApplicationData) *dockerclient.ContainerInfo {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Recovered from runContainer", sApplicationData.Name, r)
		}
	}()

	var emptyPortBinding map[string][]dockerclient.PortBinding
	var restartPolicy dockerclient.RestartPolicy
	restartPolicy.Name = "on-failure"
	restartPolicy.MaximumRetryCount = int64(3)

	//TODO: Generate name for container
	//TODO: Run new container
	hostconfig := dockerclient.HostConfig{
		PublishAllPorts: true,
		Binds:           sApplicationData.VolumeBinds,
		PortBindings:    emptyPortBinding,
		RestartPolicy:   restartPolicy,
	}

	exposedPort := make(map[string]struct{}, 1)

	if sApplicationData.Exposedport != "" {
		exposedPort[sApplicationData.Exposedport] = struct{}{}
	}

	containerConfig := &dockerclient.ContainerConfig{
		Image:        sApplicationData.Image,
		Cmd:          sApplicationData.Command,
		AttachStdin:  true,
		Tty:          false,
		HostConfig:   hostconfig,
		ExposedPorts: exposedPort,
	}
	//Make new Container Name
	if sApplicationData.IsTesting {
		sApplicationData.Name = fmt.Sprintf("%s-test", sApplicationData.Name)
	}
	ContainerName := fmt.Sprintf("%s-%d", sApplicationData.Name, time.Now().UnixNano())
	log.Println("Creating ", sApplicationData.Name, "container with ", sApplicationData.Image)
	//Create Container
	containerID, err := docker.CreateContainer(containerConfig, ContainerName)
	if err != nil {
		panic(err)
	}

	// Start the container
	err = docker.StartContainer(containerID, &hostconfig)
	if err != nil {
		panic(err)
	}
	log.Println("Starting Container Name: ", ContainerName, " ID: ", containerID)
	//Inspect the container
	var ContainerInfo *dockerclient.ContainerInfo
	ContainerInfo, err = docker.InspectContainer(containerID)
	if err != nil {
		panic(err)
	}
	return ContainerInfo
}

func buildImage(sApplication ApplicationData) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Recovered Building Image", sApplication.Name, r)
		}
	}()
	//Prepare Tar file
	//for context
	Makedockerfiletar(sApplication.DockerfilePath)
	// imageDelete, err := docker.RemoveImage(sApplication.Image, true)
	// if err != nil {
	// 	fmt.Print(err)
	// }
	// log.Println("Deleteing Image...", imageDelete)
	//TODO:Make tar of dockerfile directory
	var foundImage bool
	dockerBuildContext, err := os.Open(fmt.Sprintf("%s/%s", sApplication.DockerfilePath, "Dockerfile.tar"))
	//This code Makes runs asynchronously which is hard to catch
	//When the image is ready to run

	defer dockerBuildContext.Close()
	buildImageConfig := &dockerclient.BuildImage{
		Context:        dockerBuildContext,
		RepoName:       sApplication.Image,
		SuppressOutput: false,
		DockerfileName: "Dockerfile",
		Remove:         false,
	}
	_, err = docker.BuildImage(buildImageConfig)
	if err != nil {
		//fmt.Errorf("%s", err)
		log.Fatal("Building Image ", err)
	}
	//TODO: after building the image who knows how
	//long it will take to Finished
	for {
		images, err := docker.ListImages(false)
		if err != nil {
			log.Println(err)
		}
		foundImage = false
		for _, image := range images {
			for _, tag := range image.RepoTags {
				//log.Println(tag, sApplication.Image)
				if tag == sApplication.Image {
					foundImage = true
				}
			}
		}
		if foundImage == false {
			log.Println("Error finding built image, Might still be in progress")
		} else {
			break
		}
	}

}
func buildImageViaCLI(sApplication *ApplicationData) bool {
	response := true
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error Recovered from Building Image", sApplication.Name, r)
		}
	}()

	pathError := os.Chdir(sApplication.DockerfilePath)
	if pathError != nil {
		panic(pathError)
	}

	//Set the testing docker image as the current image
	// if sApplication.HasTest && sApplication.IsTesting {
	// 	sApplication.Image = getImageFromDockerfile(fmt.Sprintf("%s/Dockerfile", sApplication.TestDockerfilepath))
	// }

	//Get the folder that houses the current dockerfile
	//Lets name the image this name
	sApplication.Image = path.Base(sApplication.DockerfilePath)
	tmpDockerfile := fmt.Sprintf("tmp%s", sApplication.Dockerfilename)
	log.Println("Current Directory", sApplication.DockerfilePath)
	log.Println("Building: ", sApplication.Name)
	log.Println("Image: ", sApplication.Image)
	log.Println("docker", "build", "--no-cache", "-f", tmpDockerfile, "-t", sApplication.Image, ".")

	//Run Build Command
	//Name the image different than the image to stop conflicting with registry images
	reloadCommand := exec.Command(
		"docker", "build", "--no-cache",
		"-f", tmpDockerfile,
		"-t", sApplication.Image, ".")

	_, err := reloadCommand.CombinedOutput()
	if err != nil {
		panic(err)
	}

	//log.Println(string(output))
	//Sleep for 5 seconds so that the container can be finalized by docker
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
				if tag == sApplication.Image || tag == fmt.Sprintf("%s:latest", sApplication.Image) {
					log.Println("Found image", tag, sApplication.Image)
					break forever_loop
				}
			}
		}
	}
	if removeTmpDockerfile(sApplication) == false {
		panic(fmt.Sprintf("Could not remove tmpDockerfile: %s", sApplication.DockerfilePath))
	}
	return response
}

//Makedockerfiletar makes a tar out of a directory
func Makedockerfiletar(path string) bool {
	var tarMade bool
	type fileObj struct {
		Name, Body string
	}
	//Remove Existing dockerfile.tar
	os.Remove(fmt.Sprintf("%s/%s", path, "Dockerfile.tar"))
	var files = []fileObj{}

	// Create a buffer to write our archive to.
	buf := new(bytes.Buffer)

	// Create a new tar archive.
	tw := tar.NewWriter(buf)

	dir, err := os.Open(path)
	defer dir.Close()
	fileinfos, err := dir.Readdir(0)
	if err != nil {
		log.Fatal("Reading Directory: ", path, err)
	}

	for _, file := range fileinfos {
		//TODO: do not add a file that is a tar
		if strings.Contains(file.Name(), ".tar") {
			continue
		}
		b, err := ioutil.ReadFile(fmt.Sprintf("%s/%s", path, file.Name()))
		if err != nil {
			//fmt.Print(fmt.Sprintf("%s/%s", path, file.Name()))
			log.Fatal(err)
			continue
		}

		//TODO turn each file into a fileObj
		files = append(files, fileObj{
			file.Name(),
			string(b),
		})
	}

	//Gather all files and write a header and body for each file
	for _, file := range files {
		hdr := &tar.Header{
			Name: file.Name,
			Mode: 0600,
			Size: int64(len(file.Body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			log.Fatalln(err)
		}
		if _, err := tw.Write([]byte(file.Body)); err != nil {
			log.Fatalln(err)
		}
	}

	// Make sure to check the error on Close.
	if err := tw.Close(); err != nil {
		log.Fatalln(err)
	}

	err = ioutil.WriteFile(fmt.Sprintf("%s/%s", path, "Dockerfile.tar"), buf.Bytes(), 0644)
	if err != nil {
		log.Fatalln(err)
	} else {
		tarMade = true
	}
	return tarMade
}

//Reloadwebserver reloads webserver configs
//Find the ambassador webserver container
func Reloadwebserver() bool {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Panic Occured in ReloadWebserver")
			log.Println(r)
		}
	}()
	var reloaded = false
	//docker exec -it ambassador_webserver nginx -s reload
	log.Println("docker", "exec", "-t", WebserverDockerName, "nginx", "-s", "reload")
	reloadCommand := exec.Command("docker", "exec", "-t", WebserverDockerName, "nginx", "-s", "reload")
	output, err := reloadCommand.CombinedOutput()
	if err != nil {
		panic(err)
	}
	log.Println("Webserver Restart Output", string(output))
	if strings.Contains(string(output), "signal process started") == true {
		reloaded = true
	}
	return reloaded
}

//UpdateApplicationNginxConf exports nginx conf file with container Data
//for a specific application
func UpdateApplicationNginxConf(sApplicationData ApplicationData) {

	var err error
	var t = template.New("Nginx Conf")
	//TODO: write out new nginx to webserver location
	buff := bytes.NewBufferString("")
	switch sApplicationData.ConfType {
	case "phpserver.conf":
		t.Parse(phpserverconf)
	case "proxy.conf":
		t.Parse(proxyconf)
	default:
		log.Println("Could not find conf type", sApplicationData.ConfType)
	}

	err = t.Execute(buff, sApplicationData)
	//Write template to buffer
	if err != nil {
		log.Println("Error executing Template", err)
	}

	//Find filepath to export new conf file
	filePath := fmt.Sprintf("%s/%s.conf", ConfDirectory, sApplicationData.Name)
	file, err := os.Create(filePath)
	if err != nil {
		log.Fatalln("Error Opening: ", err)
	}
	defer file.Close()

	//Write contents of new conf file
	_, err = file.WriteString(buff.String())
	if err != nil {
		log.Fatal("Error Writing", err)
	}
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
