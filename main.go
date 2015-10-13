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

	"text/template"
	"time"

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
	r.ParseForm()
	repository := r.FormValue("repository")
	branchName := r.FormValue("branch")

	if repository != "" && branchName != "" {
		var bitbucketObject BitbucketPayload
		bitbucketObject.SetRepositoryName(repository)
		bitbucketObject.SetBranchName(branchName)
		sApplicationData := findManifestByName(bitbucketObject.GetRepositoryName())

		ExecutePayload(sApplicationData, bitbucketObject)
	}
	var data interface{}

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
	//t, _ := template.ParseFiles("views/manual.go")
	//t.Execute(w, data)
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
		//return nil
	}

	return sApplicationData
}

//TestApplication tests application
func TestApplication(sApplicationData *ApplicationData, bitbucketObject BitbucketPayload) {
	if sApplicationData.HasTest == false {
		return
	}
	//Set the mode of the application to test
	sApplicationData.SetTestMode(true)

	//Set the testdocker file path

	sApplicationData.DockerfilePath = sApplicationData.TestDockerfilepath

	ExecutePayload(*sApplicationData, bitbucketObject)
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

	if sApplicationData.HasTest && sApplicationData.IsTesting == false {
		go TestApplication(&sApplicationData, bitbucketObject)
	}

	//TODO: Find dockerfile location
	if _, err := os.Stat(sApplicationData.DockerfilePath); os.IsNotExist(err) {
		log.Printf("no such file or directory: %s", sApplicationData.DockerfilePath)
		return
	}

	//TODO: Replace branch from git pull command in dockerfile
	replacer := strings.NewReplacer("git clone -b hhvm", fmt.Sprintf("%s%s", "git clone -b ", bitbucketObject.GetBranchName()))
	ReplaceStringInFile(fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, "Dockerfile"), replacer)

	//TODO: build image
	buildImageViaCLI(sApplicationData)

	//TODO: Run Container
	ContainerInfo := runContainer(sApplicationData)

	StopOldContainers(sApplicationData, ContainerInfo)
	//TODO: Get container ip and port
	updateApplicationCurrentPort(&sApplicationData, ContainerInfo)

	//TODO: update nginx conf
	UpdateApplicationNginxConf(sApplicationData)

	//TODO: Reload web server
	if sApplicationData.IsTesting == false {
		//stopOldContainers()
		Reloadwebserver()
	}
}

func updateApplicationCurrentPort(sApplicationData *ApplicationData, ContainerInfo *dockerclient.ContainerInfo) {

	for portString, portBinding := range ContainerInfo.NetworkSettings.Ports {
		if portString == "80/tcp" {
			for _, binding := range portBinding {
				sApplicationData.CurrentPort = binding.HostPort
				sApplicationData.IP = ContainerInfo.NetworkSettings.IPAddress
			}
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
			if strings.Contains(name, sApplicationData.Name) == true {
				if cInfo.Name != name {
					err = docker.KillContainer(c.Id, "SIGINT")
					if err != nil {
						log.Println("Error: ", err)
					}
				}
			}
		}
	}

	//log.Println(containerNames)
	// r, e := regexp.Compile("\\d+")
	// if e != nil {
	// 	t.Error(e)
	// }

}
func runContainer(sApplicationData ApplicationData) *dockerclient.ContainerInfo {
	//TODO: Generate name for container
	//TODO: Run new container
	hostconfig := dockerclient.HostConfig{
		PublishAllPorts: true,
	}
	containerConfig := &dockerclient.ContainerConfig{
		Image:       sApplicationData.Image,
		Cmd:         sApplicationData.Command,
		AttachStdin: true,
		Tty:         false,
		HostConfig:  hostconfig,
	}
	//Make new Container Name
	r := time.Now().UnixNano()
	if sApplicationData.IsTesting {
		sApplicationData.Name = fmt.Sprintf("%s-test", sApplicationData.Name)
	}
	ContainerName := fmt.Sprintf("%s-%d", sApplicationData.Name, r)

	//Create Container
	containerID, err := docker.CreateContainer(containerConfig, ContainerName)
	if err != nil {
		log.Fatal(err)
	}

	// Start the container
	err = docker.StartContainer(containerID, &hostconfig)
	if err != nil {
		log.Fatal("Start Container: ", containerID, err)
	}
	log.Println("Container ID", containerID)
	//Inspect the container
	var ContainerInfo *dockerclient.ContainerInfo
	ContainerInfo, err = docker.InspectContainer(containerID)
	if err != nil {
		log.Fatal(err)
	}
	return ContainerInfo
}

func buildImage(sApplication ApplicationData) {
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
func buildImageViaCLI(sApplication ApplicationData) {
	var foundImage bool

	pathError := os.Chdir(sApplication.DockerfilePath)
	if pathError != nil {
		log.Fatalln(pathError)
	}
	//Set the testing docker image as the current image
	if sApplication.HasTest && sApplication.IsTesting {
		sApplication.Image = getImageFromDockerfile(fmt.Sprintf("%s/Dockerfile", sApplication.TestDockerfilepath))
	}
	log.Println("Current Directory", sApplication.DockerfilePath)
	log.Println("Building: ", sApplication.Name)
	log.Println("Image: ", sApplication.Image)
	log.Println("docker", "build", "--no-cache", "-f", sApplication.Dockerfilename, "-t", sApplication.Image, ".")

	imageName := path.Base(sApplication.DockerfilePath)
	//Run Build Command
	reloadCommand := exec.Command("docker", "build", "--no-cache", "-f", sApplication.Dockerfilename, "-t", imageName, ".")

	output, err := reloadCommand.CombinedOutput()
	if err != nil {
		log.Fatalln(err)
	}

	log.Println(string(output))
	//Sleep for 5 seconds so that the container can be finalized by docker
	time.Sleep(5 * time.Second)
	//TODO: after building the image who knows how
	//long it will take to Finished

	//for {
	images, err := docker.ListImages(false)
	if err != nil {
		log.Println(err)
	}

	for {
		foundImage = false
		for _, image := range images {
			for _, tag := range image.RepoTags {
				log.Println(tag, sApplication.Image)
				if tag == sApplication.Image || tag == fmt.Sprintf("%s:latest", sApplication.Image) {
					foundImage = true
				}
			}
		}
		if foundImage == false {
			log.Println("Error finding built image, Might still be in progress")
		} else {
			log.Println("Found image")
			break
		}
	}
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
	var reloaded = false
	//docker exec -it ambassador_webserver nginx -s reload
	reloadCommand := exec.Command("docker", "exec", "-t", WebserverDockerName, "nginx", "-s", "reload")
	output, err := reloadCommand.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	if strings.Contains(string(output), "signal process started") == true {
		reloaded = true
	}
	return reloaded
}

//UpdateApplicationNginxConf exports nginx conf file with container Data
//for a specific application
func UpdateApplicationNginxConf(sApplicationData ApplicationData) {
	t := template.New("Conf Template")
	t.ParseGlob("conf_templates/*.conf")
	t, err := t.Parse(sApplicationData.ConfType)
	if err != nil {
		log.Fatal(err)
	}
	//TODO: write out new nginx to webserver location
	buff := bytes.NewBufferString("")

	//Write template to buffer
	t.ExecuteTemplate(buff, "phpserver.conf", sApplicationData)

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

//ReplaceStringInFile replaces string inside file
//used to replace string inside docker file
func ReplaceStringInFile(filePath string, r *strings.Replacer) string {
	byteArray, err := ioutil.ReadFile(filePath)
	var newString string
	if err != nil {
		log.Fatal(err)
	}
	newString = r.Replace(string(byteArray))
	file, err := os.OpenFile(filePath, os.O_RDWR, 0666)
	if err != nil {
		log.Print("Error Opening: ", err)
	}
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

//GetRepositoryName should return the name of the git repository name
func (payload BitbucketPayload) GetRepositoryName() string {
	return payload.Repository.Name
}

//GetBranchName will return the branch name of the newest change
func (payload BitbucketPayload) GetBranchName() string {
	var branch string
	for _, change := range payload.Push.Changes {
		//fmt.Print(k, change.New.Name)
		if change.New.Name != "" {
			branch = change.New.Name
		}
	}
	return branch
}

//SetRepositoryName sets repository name
func (payload *BitbucketPayload) SetRepositoryName(name string) {
	payload.Repository.Name = name
}

//SetBranchName sets the branch name of the bitbucket payload
func (payload *BitbucketPayload) SetBranchName(branchName string) {
	for idx, change := range payload.Push.Changes {
		log.Println(change.New.Name)
		if change.New.Name != "" {
			payload.Push.Changes[idx].New.Name = branchName
		}
	}
}
