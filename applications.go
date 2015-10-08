package ambassador

//ApplicationData is the data file type used from pull data from the filesystem
type ApplicationData struct {
	Name               string
	Type               string
	Repository         string
	Image              string
	DockerfilePath     string
	LocalAppRepository string
	Hostname           string
	IP                 string
	Port               string
	HasTest            bool
	ConfType           string `json:"confType"`
	TestDockerfilepath string
	Webrootdirectory   string `json:"webrootdirectory"`
}
