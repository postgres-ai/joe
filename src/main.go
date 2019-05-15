/*
Joe provisioning part

2019 © Dmitry Udalov dmius@postgres.ai
2019 © Postgres.ai
*/
package main

import (
	"os"
	"path/filepath"

	"./ec2ctrl"
	"./log"
	"./provision"

	"github.com/tkanos/gonfig"
)

type Configuration struct {
	AwsConfiguration ec2ctrl.Ec2Configuration
	EbsVolumeId      string
	Debug            bool
	PgVersion        string
}

func main() {
	conf := Configuration{
		AwsConfiguration: ec2ctrl.Ec2Configuration{
			AwsInstanceType: "r4.large",
			AwsRegion:       "us-east-1",
			AwsZone:         "a",
		},
		Debug: true, PgVersion: "9.6"}

	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	confPath := dir + string(os.PathSeparator) + "config" + string(os.PathSeparator) + "joeprov.json"
	gonfig.GetConf(confPath, &conf)
	log.DEBUG = conf.Debug

	var err error
	//	var out string
	provConf := provision.ProvisionConfiguration{
		AwsConfiguration: conf.AwsConfiguration,
		Debug:            conf.Debug,
		EbsVolumeId:      conf.EbsVolumeId,
		PgVersion:        conf.PgVersion,
	}
	if provision.ValidateConfiguration(provConf) != true {
		log.Err("Wrong configuration.")
		os.Exit(1)
	}
	joeProvision := provision.NewProvision(provConf)
	res, sessionId, err := joeProvision.StartSession()
	if err != nil {
		log.Err("Start session error", res, sessionId, err)
	} else {
		log.Dbg("Session started", res, sessionId, err)
	}
	joeProvision.StopSession()
}
