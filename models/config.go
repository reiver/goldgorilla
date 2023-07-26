package models

type ConfigModel struct {
	LogjamBaseUrl string `json:"logjamBaseUrl"`
	TargetRoom    string `json:"targetRoom"`
}
