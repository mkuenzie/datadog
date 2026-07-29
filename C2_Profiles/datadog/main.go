package main

import (
	datadogfunctions "MyContainer/datadog/c2functions"

	"github.com/MythicMeta/MythicContainer"
)

func main() {
	// load up the agent functions directory so all the init() functions execute
	datadogfunctions.Initialize()
	// sync over definitions and listen
	MythicContainer.StartAndRunForever([]MythicContainer.MythicServices{
		MythicContainer.MythicServiceC2,
	})
}
