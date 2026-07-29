package main

import (
	"mythicDatadog/datadog_client"

	"github.com/MythicMeta/MythicContainer/logging"
)

func main() {
	datadog_client.InitializeLocalConfig()

	logging.LogInfo("Initializing datadog client")
	client := datadog_client.Initialize(datadog_client.Config)
	logging.LogInfo("Starting datadog client")
	datadog_client.Start(client)
	forever := make(chan bool)
	<-forever

}
