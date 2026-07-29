package c2functions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	c2structs "github.com/MythicMeta/MythicContainer/c2_structs"
	"github.com/MythicMeta/MythicContainer/logging"
)

type config struct {
	Debug           bool   `json:"debug"`
	AppKey          string `json:"app_key"`
	ApiKey          string `json:"api_key"`
	Region          string `json:"region"`
	ErrorFilePath   string `json:"error_file_path"`
	ErrorStatusCode int    `json:"error_status_code"`
}

func getC2JsonConfig() (*config, error) {
	currentConfig := config{}
	configBytes, err := os.ReadFile(filepath.Join(".", "datadog", "c2_code", "config.json"))
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(configBytes, &currentConfig)
	if err != nil {
		logging.LogError(err, "Failed to unmarshal config bytes")
		return nil, err
	}
	return &currentConfig, nil
}
func writeC2JsonConfig(cfg *config) error {
	jsonBytes, err := json.MarshalIndent(*cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(".", "datadog", "c2_code", "config.json"), jsonBytes, 0644)
}

var version = "0.0.1"
var datadogc2definition = c2structs.C2Profile{
	Name:             "datadog",
	Author:           "@mkuenzie",
	Description:      "Uses Datadog case management for communication",
	IsP2p:            false,
	IsServerRouted:   true,
	SemVer:           version,
	ServerBinaryPath: filepath.Join(".", "datadog", "c2_code", "mythic_datadog_server"),
	ConfigCheckFunction: func(message c2structs.C2ConfigCheckMessage) c2structs.C2ConfigCheckMessageResponse {
		response := c2structs.C2ConfigCheckMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called config check\n%v", message),
		}
		currentConfig, err := getC2JsonConfig()
		if err != nil {
			response.Success = false
			response.Message = err.Error()
			return response
		}
		_, ok := message.Parameters["api_key"]
		if !ok {
			response.Success = false
			response.Error = "Failed to get api_key attribute"
			return response
		}
		_, ok = message.Parameters["app_key"]
		if !ok {
			response.Success = false
			response.Error = "Failed to get app_key attribute"
			return response
		}
		err = writeC2JsonConfig(currentConfig)
		if err != nil {
			response.Success = false
			response.Message = err.Error()
			return response
		}
		response.Success = true
		response.Message = "Config check passed"
		response.RestartInternalServer = true
		return response
	},
	GetRedirectorRulesFunction: func(message c2structs.C2GetRedirectorRuleMessage) c2structs.C2GetRedirectorRuleMessageResponse {
		response := c2structs.C2GetRedirectorRuleMessageResponse{
			Success: false,
			Message: "Function not supported yet",
		}
		return response
	},
	OPSECCheckFunction: func(message c2structs.C2OPSECMessage) c2structs.C2OPSECMessageResponse {
		response := c2structs.C2OPSECMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called opsec check:\n%v", message),
		}
		return response
	},
	GetIOCFunction: func(message c2structs.C2GetIOCMessage) c2structs.C2GetIOCMessageResponse {
		response := c2structs.C2GetIOCMessageResponse{
			Success: true,
		}
		return response
	},
	SampleMessageFunction: func(message c2structs.C2SampleMessageMessage) c2structs.C2SampleMessageResponse {
		response := c2structs.C2SampleMessageResponse{Success: true, Message: "Function not supported yet"}

		return response
	},
	HostFileFunction: func(message c2structs.C2HostFileMessage) c2structs.C2HostFileMessageResponse {
		return c2structs.C2HostFileMessageResponse{
			Success:               false,
			RestartInternalServer: false,
			Error:                 "Function not supported yet",
		}
	},
}
var datadogc2parameters = []c2structs.C2Parameter{
	{
		Name:          "region",
		Description:   "Datadog tenant region for API calls",
		DefaultValue:  "us1",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Choices: []string{
			"us1", "us3", "us5", "eu1", "ap1", "ap2", "uk1",
		},
		Required: true,
	},
	{
		Name:          "app_key",
		Description:   "Datadog application key",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      true,
	},
	{
		Name:          "api_key",
		Description:   "Datadog API key",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      true,
	},
	{
		Name:          "killdate",
		Description:   "Kill Date",
		DefaultValue:  365,
		ParameterType: c2structs.C2_PARAMETER_TYPE_DATE,
		Required:      false,
	},
	{
		Name:          "encrypted_exchange_check",
		Description:   "Perform Key Exchange",
		DefaultValue:  true,
		ParameterType: c2structs.C2_PARAMETER_TYPE_BOOLEAN,
		Required:      false,
	},
	{
		Name:          "callback_jitter",
		Description:   "Callback Jitter in percent",
		DefaultValue:  23,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
	},
	{
		Name:          "AESPSK",
		Description:   "Encryption Type",
		DefaultValue:  "aes256_hmac",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Required:      false,
		IsCryptoType:  true,
		Choices: []string{
			"aes256_hmac",
			"none",
		},
	},
	{
		Name:          "callback_interval",
		Description:   "Callback Interval in seconds",
		DefaultValue:  10,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
	},
}

func Initialize() {
	c2structs.AllC2Data.Get("datadog").AddC2Definition(datadogc2definition)
	c2structs.AllC2Data.Get("datadog").AddParameters(datadogc2parameters)
}
