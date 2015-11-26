package main

import (
	"net/http"
	"os"
	"log"
	"fmt"
	"sync/atomic"
)

var redeployServiceIsRunning uint32 = 0
var redeployServiceShouldRunAgain uint32 = 0

func handler(responseWriter http.ResponseWriter, request *http.Request) {

	log.Printf("got request %s", request.URL.RawQuery)

	serviceToRedeploy := request.URL.Query()["service"]
	haproxy := request.URL.Query()["haproxy"][0]
	secret := request.URL.Query()["secret"][0]


	expectedSecret := os.Getenv("SEQREDEPLOY_SECRET")
	if expectedSecret == "" {
		log.Fatal("missing env SEQREDEPLOY_SECRET")
	}

	if expectedSecret != secret {
		responseWriter.WriteHeader(http.StatusUnauthorized)
		return
	}

	go triggerToRedeployServiceNowOrLater(serviceToRedeploy[0], haproxy)

	fmt.Fprintf(responseWriter, "redeploy service %s triggered", serviceToRedeploy[0])
}

func triggerToRedeployServiceNowOrLater(serviceToRedeploy string, haproxy string) {
	log.Printf("trigger to redeploy service %s", serviceToRedeploy)
	err := redeployServiceNowOrLater(serviceToRedeploy, haproxy)
	if err != nil {
		log.Print(err)
		// http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
	}
}

func redeployServiceNowOrLater(serviceNameToRedeploy string, haproxyServiceName string) error {
	if redeployServiceShouldRunAgain >= 1 {
		log.Printf("service %s already redeploying and registered to redeploy oncy again", serviceNameToRedeploy)
		return nil
	}
	return redeployServiceMaybeDeferred(serviceNameToRedeploy, haproxyServiceName)
}

func redeployServiceMaybeDeferred(serviceNameToRedeploy string, haproxyServiceName string) error {
	var err error
	var alreadyRunning bool
	for {
		alreadyRunning, err = redeployServiceRunOnceAtATime(serviceNameToRedeploy, haproxyServiceName)
		if (alreadyRunning) {
			atomic.SwapUint32(&redeployServiceShouldRunAgain, 1)
			log.Printf("service %s already redeploying: registered to redeploy oncy again later", serviceNameToRedeploy)
			return nil
		}
		if err != nil {
			log.Printf("redeploy service %s resulted in error: %s", serviceNameToRedeploy, err)
		}
		if ! redeployServiceShouldRunAgainThenReset()  {
			break
		}
		log.Printf("redeploy service %s done. Now run again...", serviceNameToRedeploy)
	}
	return err
}

func redeployServiceRunOnceAtATime(serviceNameToRedeploy string, haproxyServiceName string) (alreadyRunning bool, err error) {

	if RedeployServiceIsNotRunningThenSet() {
		defer atomic.SwapUint32(&redeployServiceIsRunning, 0)
		err := redeployService(serviceNameToRedeploy, haproxyServiceName)
		atomic.SwapUint32(&redeployServiceIsRunning, 0);

		return false, err
	} else {
		// is already running
		return true, nil
	}
}

func RedeployServiceIsNotRunningThenSet() bool {
	return atomic.CompareAndSwapUint32(&redeployServiceIsRunning, 0, 1)
}

func redeployServiceShouldRunAgainThenReset() bool {
	return atomic.CompareAndSwapUint32(&redeployServiceShouldRunAgain, 1, 0)
}

func main() {
	// example: /redeploy/?service=tuerauf-prod&haproxy=tuerauf-haproxy&secret=secret_abc123
	log.Printf("starting http server now...")
	http.HandleFunc("/redeploy/", handler)
	http.ListenAndServe(":8080", nil)
}
