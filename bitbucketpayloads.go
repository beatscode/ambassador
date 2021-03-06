package main

//NewChange is an embedded object of the change object
type NewChange struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Date    string `json:"date"`
}

//Change represents the changes object in a push payload
type Change struct {
	Closed  bool      `json:"closed"`
	Created bool      `json:"created"`
	New     NewChange `json:"new"`
	Old     NewChange `json:"old"`
}

//Push is a push payload struct
type Push struct {
	Changes []Change `json:"changes"`
}

//Source is an embedded map of pullrequest payload
type Source struct {
	Branch     map[string]map[string]string `json:"branch"`
	Commit     map[string]map[string]string `json:"commit"`
	Repository Repository                   `json:"repository"`
}

//Repository is the bitbucket pull request payload
type Repository struct {
	FullName string                 `json:"full_name"`
	Links    map[string]interface{} `json:"links"`
	Name     string                 `json:"name"`
	Scm      string                 `json:"scm"`
}

//Pullrequest is the bitbucket pull request payload
type Pullrequest struct {
	ID          int               `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Source      Source            `json:"source"`
	Destination Source            `json:"destination"`
	State       string            `json:"state"`
	Author      string            `json:"author"`
	MergeCommit map[string]string `json:"merge_commit"`
	CreatedOn   string            `json:"created_on"`
}

//BitbucketPayload is the whole payload from bitbucket
type BitbucketPayload struct {
	Actor       interface{} `json:"actor"`
	Repository  Repository  `json:"repository"`
	Push        Push        `json:"push"`
	Pullrequest Pullrequest `json:"pullrequest"`
}

//GetRepositoryName should return the name of the git repository name
func (payload BitbucketPayload) GetRepositoryName() string {
	return payload.Repository.Name
}

//GetBranchName will return the branch name of the newest change
func (payload BitbucketPayload) GetBranchName() string {
	var branch string
	for _, change := range payload.Push.Changes {
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

//NewBitbucketPayload returns a new bitbucket
func NewBitbucketPayload() BitbucketPayload {
	var bb BitbucketPayload
	var push Push
	var changes []Change
	var newChange NewChange
	changes = append(changes, Change{
		New: newChange,
	})
	push.Changes = changes
	bb.Push = push
	return bb
}

//SetBranchName sets the branch name of the bitbucket payload
func (payload *BitbucketPayload) SetBranchName(branchName string) {
	for idx := range payload.Push.Changes {
		payload.Push.Changes[idx].New.Name = branchName
	}
}
