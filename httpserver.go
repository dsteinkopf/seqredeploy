package main

import (
	"net/http"
	"os"
	"log"
	"fmt"
	"sync/atomic"
"github.com/tutumcloud/go-tutum/tutum"
)

var redeployServiceIsRunning uint32 = 0
var redeployServiceShouldRunAgain uint32 = 0

func redeployHandler(responseWriter http.ResponseWriter, request *http.Request) {

	log.Printf("got request %s", request.URL.RawQuery)

	serviceToRedeploy := request.URL.Query()["service"]
	haproxy := request.URL.Query()["haproxy"][0]

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

type handler func(responseWriter http.ResponseWriter, request *http.Request)

func checkSecret(pass handler) handler {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		secret := request.URL.Query()["secret"][0]

		expectedSecret := os.Getenv("SEQREDEPLOY_SECRET")
		if expectedSecret == "" {
			http.Error(responseWriter, "missing env SEQREDEPLOY_SECRET", http.StatusUnauthorized)
			return
		}

		if expectedSecret != secret {
			http.Error(responseWriter, "bad secret", http.StatusUnauthorized)
			return
		}

		pass(responseWriter, request)
	}
}

func healthHandler(responseWriter http.ResponseWriter, request *http.Request) {

	log.Printf("got health request %s", request.URL.RawQuery)

	// do some tutum call to check health
	serviceList, err := tutum.ListServices()
	if err != nil {
		log.Printf("tutum problem: %s", err)
		http.Error(responseWriter, "tutum problem", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(responseWriter, "ok. len(serviceList)=%d", len(serviceList.Objects))
}

func main() {
	log.Printf("starting http server now...")

	// example: /redeploy/?service=tuerauf-prod&haproxy=tuerauf-haproxy&secret=secret_abc123
	http.HandleFunc("/redeploy/", checkSecret(redeployHandler))

	// example: /redeploy/health/?secret=secret_abc123
	http.HandleFunc("/redeploy/health/", checkSecret(healthHandler))

	http.ListenAndServe(":8080", nil)
}
