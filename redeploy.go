package main

import (
	"github.com/tutumcloud/go-tutum/tutum"
	"log"
	"syscall"
	"net/http"
	"time"
	"fmt"
	"strconv"
	"strings"
	"os"
)

func main_hide() {
	err := redeployService("tuerauf-prod", "tuerauf-haproxy")
	if err != nil {
		log.Fatal(err)
		syscall.Exit(1);
	}
}

func redeployService(serviceNameToRedeploy string, haproxyServiceName string) error {
	// tutum User, ApiKey should be set in environment TUTUM_USER, TUTUM_APIKEY

	serviceList, err := tutum.ListServices()
	if err != nil {
		return err
	}

	redeployedServiceUuid, err := getServiceUuid(serviceNameToRedeploy, serviceList.Objects)
	if err != nil {
		return err
	}
	haproxyServiceUuid, err := getServiceUuid(haproxyServiceName, serviceList.Objects)
	if err != nil {
		return err
	}
	haproxyContainerUuid, err := getServicesFirstContainerUuid(haproxyServiceUuid)
	if err != nil {
		return err
	}

	redeployedService, err := tutum.GetService(redeployedServiceUuid)
	if err != nil {
		return err
	}

	log.Printf("redeploying service %s (%s) via haproxy %s (%s)",
		redeployedService.Name, redeployedServiceUuid, haproxyServiceName, haproxyServiceUuid)

	for i := range redeployedService.Containers {
		err = doRedeployContainer(redeployedService.Containers[i], redeployedService, haproxyContainerUuid)
		if err != nil {
			return err
		}
		time.Sleep(25 * time.Second)
	}

	// future improvement: do final check via haproxy

	log.Printf("successfully redeployed service %s (%s) via haproxy %s (%s)",
		redeployedService.Name, redeployedServiceUuid, haproxyServiceName, haproxyServiceUuid)

	return nil
}

func getServiceUuid(serviceName string, serviceArray []tutum.Service) (string, error) {

	for i := range serviceArray {
		var service = serviceArray[i];
		if service.Name == serviceName {
			return service.Uuid, nil
		}
	}
	return "", fmt.Errorf("Service %s not found", serviceName)
}

func getServicesFirstContainerUuid(serviceUuid string) (string, error) {
	service, err := tutum.GetService(serviceUuid)
	if err != nil {
		return "", err
	}
	return getUuidFromUri(service.Containers[0]), nil // ??? URI or Uuid?
}

func getUuidFromUri(uri string) string {
	return uri; // no conversion seems necessary
}

func getEnvFromContainer(envName string, container tutum.Container) (string, error) {
	for i := range container.Container_envvars {
		env := container.Container_envvars[i];
		if env.Key == envName {
			return env.Value, nil
		}
	}
	return "", fmt.Errorf("Env %s not found in Container %s", envName, container.Name)
}

func doRedeployContainer(containerUri string, parentService tutum.Service, haproxyContainerUuid string) error {
	container, err := tutum.GetContainer(getUuidFromUri(containerUri))
	if err != nil {
		return err
	}
	httpCheckDef, err := getEnvFromContainer("HTTP_CHECK", container)
	if err != nil {
		return err
	}
	parts := strings.Split(httpCheckDef, " ")
	if len(parts) < 2 || parts[0] != "GET" {
		return fmt.Errorf("container %s (%s) has bad HTTP_CHECK '%s' (not implemented)",
			container.Name, containerUri, httpCheckDef)
	}
	httpCheckUri := parts[1];

	log.Printf("redeploying container %s (%s)", container.Name, container.Uuid)

	err = container.Redeploy(tutum.ReuseVolumesOption{Reuse: true})
	if err != nil {
		return err
	}

	newContainer, err := waitForContainerToTerminateAndReappear(container, parentService)
	if err != nil {
		return err
	}

	err = waitForCheckOk(httpCheckUri, newContainer, 10*60)
	if err != nil {
		return err
	}
	log.Printf("container %s (%s) checked ok", newContainer.Name, newContainer.Uuid)
	return nil
}

func waitForCheckOk(httpCheckUri string, container tutum.Container, timeoutSeconds int64) error {
	log.Printf("waitForCheckOk")
	startedAt := getTimestampSeconds()

	hostIp := os.Getenv("SEQREDEPLOY_HOSTIP")
	if hostIp == "" {
		hostIp = container.Private_ip
	}
	tcpPort := container.Container_ports[0].Outer_port
	fullUrl := "http://" + hostIp + ":" + strconv.Itoa(tcpPort) + httpCheckUri

	for {
		log.Printf("container %s (%s): calling %s", container.Name, container.Uuid, fullUrl)
		response, err := http.Get(fullUrl)
		if err != nil {
			log.Print(err)
		} else {
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
			if getTimestampSeconds() - startedAt >= timeoutSeconds {
				return fmt.Errorf("container %s (%s): timeout calling %s", container.Name, container.Uuid, fullUrl)
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func getTimestampSeconds() int64 {
	return time.Now().UnixNano() / int64(time.Second)
}

func waitForContainerToTerminateAndReappear(container tutum.Container,
											parentService tutum.Service) (tutum.Container, error) {
	log.Printf("waitForContainerToTerminateAndReappear")
	eventChan := make(chan tutum.Event)
	errChan := make(chan error)

	go tutum.TutumEvents(eventChan, errChan)

	didTerminate := false
	var newContainer tutum.Container
	var err error

	for {
		select {
		case event := <-eventChan:
			// log.Println(event)
			if event.Type == "container" &&
				strings.Contains(event.Resource_uri, container.Uuid) &&
				event.State == "Terminated"	{

				log.Printf("got container Terminated event on my container")
				didTerminate = true
			}
			if event.Type == "container" &&
				parentsContain(event.Parents, parentService.Uuid) &&
				event.State == "Running" &&
				didTerminate == true {

				newContainer, err = tutum.GetContainer(getUuidFromUri(event.Resource_uri))
				if err != nil {
					return newContainer, err
				}
				log.Printf("got container Running event on new container (%s)", newContainer.Uuid)
				return newContainer, nil
			}
		case err := <-errChan:
			return newContainer, err
		}
	}
}

func parentsContain(parents []string, lookedFor string) bool {
	for i := range parents {
		if strings.Contains(parents[i], lookedFor) {
			return true
		}
	}
	return false
}

// func hashArraySearchForObjectContainingKeyValue(hashArray )
