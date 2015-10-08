package ambassador

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"text/template"

	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
)

//ApplicationDataPath
var ApplicationDataPath string

//ConfDirectory houses the updated conf files
var ConfDirectory string
var docker *dockerclient.DockerClient

func main() {
	docker, _ = dockerclient.NewDockerClient("unix:///var/run/docker.sock", nil)

	flag.StringVar(&ApplicationDataPath, "applicationDataPath", "./applicationDataFiles", "")
	fmt.Println("Application Data Path", ApplicationDataPath)
	flag.StringVar(&ConfDirectory, "confDirectory", ".", "")
	flag.Parse()

	r := mux.NewRouter()
	r.HandleFunc("/ambassador/webhookchange", AppchangeHandler).Methods("POST")
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

	var foundApp = false
	//Unmarshal into Bitbucket Payload object
	var bitbucketObject BitbucketPayload
	var sApplicationData ApplicationData

	//Parse form and unmarshal payload
	r.ParseForm()
	jsonByteArray, err := json.Marshal(r.Form)
	if err != nil {
		log.Print(err)
	}

	//parses the JSON-encoded data
	err = json.Unmarshal(jsonByteArray, &bitbucketObject)
	if err != nil {
		log.Print(err)
		logit(jsonByteArray)
	}

	//Parse Manifest Files for the appropriate application
	manifests := loadApplicationDataFiles()

	for _, manifest := range manifests {
		if strings.ToLower(manifest.Name) == strings.ToLower(bitbucketObject.GetRepositoryName()) {
			foundApp = true
			sApplicationData = manifest
		}
	}

	if !foundApp {
		log.Print("Could not find manifest file for", bitbucketObject.GetRepositoryName())
		return
	}

	//TODO: Find dockerfile location
	if _, err := os.Stat(sApplicationData.DockerfilePath); os.IsNotExist(err) {
		log.Printf("no such file or directory: %s", sApplicationData.DockerfilePath)
		return
	}

	//TODO: Replace branch from git pull command in dockerfile
	replacer := strings.NewReplacer("git clone -b hhvm", fmt.Sprintf("%s%s", "git clone -b ", bitbucketObject.GetBranchName()))
	ReplaceStringInFile(sApplicationData.DockerfilePath, replacer)

	//TODO: build image
	dockerBuildContext, err := os.Open(sApplicationData.DockerfilePath)
	defer dockerBuildContext.Close()
	buildImageConfig := &dockerclient.BuildImage{
		Context:        dockerBuildContext,
		RepoName:       bitbucketObject.GetRepositoryName(),
		SuppressOutput: false,
	}
	_, err = docker.BuildImage(buildImageConfig)
	if err != nil {
		log.Fatal(err)
	}

	//TODO: Generate name for container
	//TODO: Run new container
	containerConfig := &dockerclient.ContainerConfig{
		Image:       sApplicationData.Image,
		Cmd:         nil,
		AttachStdin: true,
		Tty:         false}

	ContainerName := fmt.Sprintf("leo%s", sApplicationData.Name)
	containerID, err := docker.CreateContainer(containerConfig, ContainerName)
	if err != nil {
		log.Fatal(err)
	}

	// Start the container
	hostConfig := &dockerclient.HostConfig{}
	err = docker.StartContainer(containerID, hostConfig)
	if err != nil {
		log.Fatal(err)
	}
	var ContainerInfo *dockerclient.ContainerInfo
	ContainerInfo, err = docker.InspectContainer(containerID)
	if err != nil {
		log.Fatal(err)
	}

	//TODO: Get container ip and port
	sApplicationData.IP = ContainerInfo.NetworkSettings.IPAddress
	for _, portBindings := range ContainerInfo.NetworkSettings.Ports {
		for _, port := range portBindings {
			sApplicationData.Port = port.HostPort
		}
	}

	//TODO: update nginx conf
	UpdateApplicationNginxConf(sApplicationData)

	//Reload web server
	Reloadwebserver()
}

//Reloadwebserver reloads webserver configs
//Find the ambassador webserver container
func Reloadwebserver() {
	//docker exec -it ambassador_webserver nginx -s reload

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
		log.Print(err)
	}
	newString = r.Replace(string(byteArray))
	file, err := os.OpenFile(filePath, os.O_RDWR, 0666)
	if err != nil {
		log.Print("Error Opening", err)
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
