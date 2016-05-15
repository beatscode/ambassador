package main

//ApplicationData is the data file type used
//to pull metadata from the filesystem
type ApplicationData struct {
	Name                  string
	Type                  string
	Repository            string
	Image                 string
	DockerfilePath        string
	Dockerfilename        string
	LocalAppRepository    string
	Command               []string
	Hostname              string
	IP                    string
	Exposedport           string `json:"exposedport"`
	CurrentPort           string
	HasTest               bool
	ConfType              string `json:"confType"`
	TestDockerfilepath    string
	Webrootdirectory      string `json:"webrootdirectory"`
	IsTesting             bool
	VolumeBinds           []string `json:"volumebinds"`
	DockercomposeBuildCmd []string `json:"dockercomposebuildcmd"`
	DockercomposeTestCmd  []string `json:"dockercomposetestcmd"`
	DockercomposeRunCmd   []string `json:"dockercomposeruncmd"`
}

//SetTestMode sets the mode of the application when executing Payload
func (app *ApplicationData) SetTestMode(mode bool) {
	app.IsTesting = mode
}
