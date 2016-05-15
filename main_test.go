package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"testing"

	"github.com/samalba/dockerclient"
)

var bitbucketObject BitbucketPayload
var sApplicationData ApplicationData

func init() {
	byteArray, err := ioutil.ReadFile("bitbucket.push.json")
	if err != nil {
		panic(err)
	}

	json.Unmarshal(byteArray, &bitbucketObject)
	ConfDirectory = "./testConf"

	ApplicationDataPath = "./applicationDataFiles"

	docker, _ = dockerclient.NewDockerClient("unix:///var/run/docker.sock", nil)

	manifests := loadApplicationDataFiles()

	bitbucketObject.Repository.Name = "test"

	for _, manifest := range manifests {
		if strings.ToLower(manifest.Name) == strings.ToLower(bitbucketObject.GetRepositoryName()) {
			sApplicationData = manifest
		}
	}

	//Start nginx
	// nginxTest := sApplicationData
	// nginxTest.Name = "ambassador_webserver"
	// nginxTest.Image = "nginx:latest"
}

func TestBitbucketPushLookup(t *testing.T) {

	if bitbucketObject.Repository.Name == "test" {
		t.Log("Repository Name Passed")
	}

	if len(bitbucketObject.Push.Changes) > 0 {
		t.Log("Found Changes Slice")
	} else {
		t.Error("Error finding Push CHanges")
	}
	if bitbucketObject.GetBranchName() == "master" {
		t.Log("Found Branch Name successfully")
	} else {
		t.Error("Could not find correct branch name")
	}
}
func TestReplaceGitBranchPhrase(t *testing.T) {
	branchPhrase := []byte("git clone -b dev")
	newbranchPhrase := "git clone -b test"
	r, e := regexp.Compile("git clone -b \\w+")
	if e != nil {
		t.Error(e)
	}
	newByteArray := r.ReplaceAll([]byte(branchPhrase), []byte(newbranchPhrase))
	fmt.Println("New Phrase", string(newByteArray))
	if string(newByteArray) == newbranchPhrase {
		t.Log("Success")
	} else {
		t.Error("Error converting", string(newByteArray))
	}
}
func TestReplaceStringInFile(t *testing.T) {

	sApplicationData.DockerfilePath = "testdockerfiledirectory"
	sApplicationData.Image = "busybox"
	sApplicationData.Dockerfilename = "Dockerfile.busybox"

	s := ReplaceStringInFile(&sApplicationData, bitbucketObject)

	if strings.Contains(s, fmt.Sprintf("%s%s", "git clone -b ", bitbucketObject.GetBranchName())) {
		t.Log("Success")
	} else {
		t.Error("Failed to replace string in file")
	}
}

func TestListContainers(t *testing.T) {
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	fmt.Print(containers)
	if len(containers) > 0 {
		t.Log("Success")
	} else {
		t.Error("Could not get any containers")
	}
}

//TODO:Skipping till I hear an answer for
//https://github.com/samalba/dockerclient/issues/173
func sTestExec(t *testing.T) {
	//var sContainer dockerclient.Container
	var config dockerclient.ExecConfig
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/testapi" {
				config.Container = c.Id
			}
		}
	}
	if config.Container == "" {
		t.Error("Container Not found")
	}
	config.Cmd = []string{"bash", "-c", "date"}
	config.AttachStdout = true
	config.AttachStderr = true
	config.AttachStdin = false
	config.Tty = false
	config.Detach = false
	ID, err := docker.ExecCreate(&config)
	fmt.Println(ID)
	if err != nil {
		t.Error(err)
	}

	config.Cmd = []string{}
	config.AttachStdout = true
	config.AttachStderr = true
	config.AttachStdin = true
	config.Tty = true
	config.Detach = true
	//fmt.Print(config)
	err = docker.ExecStart(ID, &config)
	if err != nil {
		t.Error(err)
	}
	//fmt.Print(containers)
}

func TestRunDockerExec(t *testing.T) {
	var containerID string
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {

			if strings.Contains(name, WebserverDockerName) == true {
				fmt.Println(name, WebserverDockerName)
				containerID = c.Id
			}
		}
	}

	if containerID == "" {
		t.Error("Could not find ", WebserverDockerName)
	}

	reloadCommand := exec.Command("docker", "exec", "-t", containerID, "nginx", "-s", "reload")
	output, err := reloadCommand.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	if strings.Contains(string(output), "signal process started") == true {
		t.Log("Success")
	} else {
		t.Error("Error")
	}
}

func TestTmpDockerfile(t *testing.T) {
	sApplicationData.DockerfilePath = "testdockerfiledirectory"
	sApplicationData.Image = "busybox"
	sApplicationData.Dockerfilename = "Dockerfile.busybox"
	newBranchString := fmt.Sprintf("%s%s", "git clone -b ", bitbucketObject.GetBranchName())

	ReplaceStringInFile(&sApplicationData, bitbucketObject)

	tmpfilepath := fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, sApplicationData.Dockerfilename)
	if strings.Contains(sApplicationData.Dockerfilename, "tmp") != true {
		t.Error("Dockerfilename was not changed properly")
	}
	if _, err := os.Stat(fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, sApplicationData.Dockerfilename)); os.IsNotExist(err) {
		t.Error("tmpDockerfile was not created")
	}

	//Check that the branch was changed
	byteArray, _ := ioutil.ReadFile(tmpfilepath)
	if strings.Contains(string(byteArray), newBranchString) {
		t.Log("Success")
	} else {
		t.Error("Could not find new branch replacement in dockerfile")
	}

	removeTmpDockerfile(&sApplicationData)
	if _, err := os.Stat(fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, sApplicationData.Dockerfilename)); os.IsNotExist(err) {
		t.Log("Success")
	} else {
		t.Error("tmpDockerFile still exists after attempted removal")
	}
}
func TestBrokenBuild(t *testing.T) {

	// TODO: tried using api
	// var reponame = "testyimage:latest"
	// TODO: build image
	sApplicationData.DockerfilePath = "testdockerfiledirectory"
	sApplicationData.Image = "busybox"
	sApplicationData.Dockerfilename = "Dockerfile.fail"

	if buildImageViaCLI(&sApplicationData) {
		t.Error("This image was supposed to fail")
	} else {
		t.Log("Successfully returned false due to faulty build")
	}

}

//Build image from docker compose exec
func TestBuildImageViaDockerCompose(t *testing.T) {

	//	log.Println(sApplicationData.DockercomposeBuildCmd)
	args := []string{"-f", "/Users/alex/projects/mednet/dockerfiles/leonardo_docker_compose/docker-compose.yml",
		"-f", "/Users/alex/projects/mednet/dockerfiles/leonardo_docker_compose/docker-compose.dev.yml", "ps"}
	buildCommand := exec.Command("docker-compose", args...)
	buildOutput, err := buildCommand.CombinedOutput()
	log.Println(string(buildOutput))
	if err != nil {
		panic(err)
	}
	//	if buildImageViaCLI(&sApplicationData) {
	//		t.Error("This image was supposed to fail")
	//	} else {
	//		t.Log("Successfully returned false due to faulty build")
	//	}

}

func TestReadApplicationData(t *testing.T) {
	ApplicationDataPath = "./applicationDataFiles"
	manifests := loadApplicationDataFiles()
	if len(manifests) > 0 {
		t.Log("Success")
	} else {
		t.Error("Error")
	}
	fmt.Println(manifests)

}

func TestReadTestImage(t *testing.T) {

	imageName := getImageFromDockerfile("testdockerfiledirectory/Dockerfile.busybox")

	if imageName == "busybox" {
		t.Log("Success")
	} else {
		t.Error("Failed to find correct image", imageName)
	}

}

func TestManualBitbucket(t *testing.T) {
	branchName := "testybaby"
	var bb = NewBitbucketPayload()
	bb.SetBranchName(branchName)
	bb.SetRepositoryName("test")
	if bb.GetBranchName() != branchName {
		t.Error("Wrong BranchName", bb.GetBranchName())
	}
	if bb.GetRepositoryName() != "test" {
		t.Error("Invalid Repository Name")
	}
}
func TestApplicationTest(t *testing.T) {
	//Check for hasTest flag
	//Build the image test docker file path
	//Run Container
	branchName := "testybaby"

	sApplicationData.TestDockerfilepath = "testdockerfiledirectory"
	sApplicationData.Dockerfilename = "Dockerfile.busybox"
	sApplicationData.Name = "test" // Repository Name
	sApplicationData.Image = "busybox:latest"
	bitbucketObject.SetBranchName(branchName)
	bitbucketObject.SetRepositoryName("test")
	sApplicationData.Command = []string{"/bin/sh", "-c", "while :; do echo 'Hit CTRL+C'; sleep 1; done"}
	TestApplication(sApplicationData, bitbucketObject)

	dockerFilePath := fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, "Dockerfile")

	byteArray, err := ioutil.ReadFile(dockerFilePath)
	if err != nil {
		t.Error(err)
	}

	if strings.Contains(string(byteArray), fmt.Sprintf("git clone -b %s", branchName)) {
		t.Log("Success")
	} else {
		t.Error("Error")
	}

	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}

	var found bool
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.Contains(name, fmt.Sprintf("%s-test-", sApplicationData.Name)) == true {
				found = true
			}
		}
	}

	if found == false {
		t.Error("Could not find container")
	}
}

func something() bool {
	res := true
	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
			log.Println("response", res)
			res = true
		}
	}()

	if 1+2 == 2 {
		panic("Im panicing")
	}
	return res
}

func TestRemoveDeadImages(t *testing.T) {
	//var containerName = "<none>"
	RemoveDeadImages()
	images, err := docker.ListImages(true)
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}

	imageNames := []string{}
	for _, i := range images {
		for _, r := range i.RepoTags {
			if r == "<none>:<none>" {
				imageNames = append(imageNames, i.Id)
			}
		}
	}
	if len(imageNames) > 1 {
		t.Error("Still numerous dead images alive")
	} else {
		t.Log("Success")
	}
}
