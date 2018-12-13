package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	numRequest     = 1600
	functionName   = "load-test"
	namespace      = "load-test"
	functionYaml   = "k8syaml/function.yaml"
	nsYaml         = "k8syaml/ns.yaml"
	expectedOutput = "Call to the function load-test was successful!"
)

var (
	slackToken    = "gZJI7risPpW67frP3EiDrPV0"
	slackChaneel  = "c4-xf-load-test"
	endpoint      = fmt.Sprintf("http://%s.%s:8080", functionName, namespace)
	slackEndpoint = "https://sap-cx.slack.com/services/hooks/jenkins-ci/"
	client        = getHttpClient(true)
	slack         *Slack
	testResult    *TestResult
	timeout       = time.After(time.Duration(5) * time.Minute)
	durationtime  = 5
	stopping      = false
	mutex         sync.RWMutex
)

type Slack struct {
	SlackEndpoint string
	SlackChaneel  string
}
type TestResult struct {
	sync.RWMutex
	resultMessage         string
	errorResponse         string
	errorRequest          string
	numFailedRequests     int
	numSuccessfulRequests int
	totalRequests         int
}

// TODO: testResult is shared among goroutines, need to handle
func main() {
	log.Println("starting horizontal pod autoscaler test for functions")
	numCPUs := runtime.GOMAXPROCS(runtime.NumCPU())
	log.Println("number of logical CPUs: ", runtime.NumCPU())
	start := time.Now()
	tick := time.Tick(1 * time.Second)
	calculateExecutionTime()

	respCh := make(chan string)
	doneCh := make(chan bool)
	var wg sync.WaitGroup
	wg.Add(numCPUs)
	for c := 0; c < numCPUs; c++ {
		go func() {
			defer wg.Done()
			for r := 0; r < numRequest; r++ {
				mutex.RLock()
				if stopping {
					mutex.RUnlock()
					break
				}
				makeHttpRequest(respCh)
			}
		}()
	}
	go func() {
		printResponse(respCh, doneCh)
	}()
	go func() {
		wg.Wait()
		close(respCh)
	}()
	for {

		select {
		case <-timeout:
			mutex.Lock()
			stopping = true
			mutex.Unlock()
			closingTest(start)
			log.Fatalf("load test timed out!")
		case <-tick:
			//log.Println("processing")
		case <-doneCh:
			closingTest(start)
			log.Println("done Channel closed")
			break
		}
	}

}

func closingTest(start time.Time) {
	checkFunctionAutoscaled()
	slack.sendNotificationtoSlackChannel(testResult)
	log.Println("finishing horizontal pod autoscaler test for functions")
	log.Printf("%.2fm elapsed\n", time.Since(start).Minutes())
	cleanup()
}

func init() {
	//cleanup()
	log.Printf("create namespace %s \n", namespace)
	createNS()
	log.Printf("deploying %s function \n", functionName)
	deployFun()
	log.Printf("verifying correct function output for %s \n", functionName)
	log.Printf("endpoint for the function: %v\n", endpoint)
	ensureOutputIsCorrect()
	slack = NewSlack()
	testResult = NewTestResult()
}

func calculateExecutionTime() {
	execTimeout := os.Getenv("LOAD_TEST_EXECUTION_TIMEOUT")
	if len(execTimeout) > 0 {
		executionTimeOut, err := strconv.Atoi(execTimeout)
		if err != nil {
			log.Printf("error on getting env variable for LOAD_TEST_EXECUTION_TIMEOUT: %v", execTimeout)
			log.Printf("current execution timeout %v", executionTimeOut)
		}
		if executionTimeOut > 5 {
			timeout = time.After(time.Duration(executionTimeOut) * time.Minute)
			durationtime = executionTimeOut
		}

	}
	log.Printf("%v minutes timeout are configured for the execution of load-test", durationtime)
}

func createNS() {
	stdoutStderr, err := deployK8s(nsYaml)
	if err != nil {
		log.Fatal("unable to create namespace ", namespace, ":\n", string(stdoutStderr))
	}
}

func deployFun() {
	stdoutStderr, err := deployK8s(functionYaml)
	if err != nil {
		log.Fatal("unable to deploy function ", functionName, ":\n", string(stdoutStderr))
	}
	log.Printf("verifying that function %s is correctly deployed.\n", functionName)
	ensureFunctionIsRunning()
}

func ensureFunctionIsRunning() {
	timeout := time.After(10 * time.Minute)
	tick := time.Tick(1 * time.Second)
	log.Println("10 minutes timeout for this operation.")
	for {
		select {
		case <-timeout:
			cmd := exec.Command("kubectl", "-n", namespace, "describe", "pod", "-l", "function="+functionName)
			stdoutStderr, _ := cmd.CombinedOutput()
			log.Fatalf("timed out waiting for: %v function pod to be running. Because of following error: %v ", functionName, string(stdoutStderr))
		case <-tick:
			cmd := exec.Command("kubectl", "-n", namespace, "get", "pod", "-l", "function="+functionName, "-ojsonpath={range .items[*]}{.status.phase}{end}")
			stdoutStderr, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("error while fetching the status phase of the function pod when verifying function is running: %v", string(stdoutStderr))
			}
			functionPodsCmd := exec.Command("kubectl", "-n", namespace, "get", "pod", "-l", "function="+functionName, "-ojsonpath={.items[0].metadata.name}")
			functionPodName, err := functionPodsCmd.CombinedOutput()
			if err != nil {
				log.Printf("error in fetching function pod when verifying function is running: %v", string(functionPodName))
			}
			hpaOutput, err := checkFunctionHpa()
			if err != nil {
				log.Printf("error in fetching function hpa when verifying function is running: %v", err)
			}
			if err == nil && strings.Contains(string(stdoutStderr), "Running") {
				log.Printf("pod: %v: is running!", string(functionPodName))
				log.Printf("hpa: %v: is running! \n", string(hpaOutput))
				return
			}
		}
	}
}

func ensureOutputIsCorrect() {
	timeout := time.After(10 * time.Minute)
	tick := time.Tick(5 * time.Second)
	log.Println("10 minutes timeout for this operation.")
	for {
		select {
		case <-timeout:
			log.Fatalf("timeout: test hpa failed!")
		case <-tick:
			resp, err := client.Get(endpoint)
			if err != nil {
				log.Printf("unable to call host: %v : Error: %v", endpoint, err)
			} else {
				if resp.StatusCode == http.StatusOK {
					bodyBytes, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						log.Fatalf("unable to get response: %v", err)
					}
					log.Printf("response from function: %v\n", string(bodyBytes))
					functionPodsCmd := exec.Command("kubectl", "-n", namespace, "get", "pod", "-l", "function="+functionName, "-ojsonpath={.items[0].metadata.name}")
					functionPodName, err := functionPodsCmd.CombinedOutput()
					if err != nil {
						log.Printf("error in fetch function pod when verifying correct output: %v", string(functionPodName))
					}
					if strings.Contains(string(bodyBytes), expectedOutput) {
						log.Printf("response contains output: %v == %v", string(bodyBytes), expectedOutput)
						log.Printf("name of the successful pod is: %v", string(functionPodName))
						return
					}
					log.Printf("name of the failed pod is: %v", string(functionPodName))
					log.Fatalf("response is not equal to expected output:\nResponse: %v\nExpected: %v", string(bodyBytes), expectedOutput)
				} else {
					log.Printf("response from function: %v", resp.StatusCode)
					bodyBytes, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						log.Printf("unable to get response: %v", err)
					}
					log.Printf("response body is: %v", string(bodyBytes))
				}
			}
		}
	}
}

func deployK8s(yamlFile string) (string, error) {
	cmd := exec.Command("kubectl", "create", "-f", yamlFile, "-n", namespace)
	stdoutStderr, err := cmd.CombinedOutput()
	output := string(stdoutStderr)
	return output, err
}

func printResponse(respCh chan string, doneCh chan bool) {
	for resp := range respCh {
		log.Println(resp)
	}
	doneCh <- true
	log.Println("all requests executed!")
}

const lettersAndNums = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = lettersAndNums[rand.Intn(len(lettersAndNums))]
	}
	return string(b)
}

func makeHttpRequest(respCh chan<- string) {
	testResult.Lock()
	start := time.Now()
	testID := randomString(8)
	resp, err := http.Post(endpoint, "text/plain", bytes.NewBuffer([]byte(testID)))
	secs := time.Since(start).Seconds()
	if err != nil {
		testResult.errorRequest = fmt.Sprintf("%.2f elapsed with error on response [ERROR] %v", secs, err)
		respCh <- testResult.errorRequest
		testResult.numFailedRequests++
		return
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		testResult.errorResponse = fmt.Sprintf("%.2f elapsed with error Unable to get response [ERROR] %v", secs, err)
		respCh <- testResult.errorResponse
		testResult.numFailedRequests++
		return
	}
	if resp.StatusCode != http.StatusOK {
		testResult.errorResponse = fmt.Sprintf("%.2f elapsed with not 200 response. response code: %v endpoint: %s", secs, resp.StatusCode, endpoint)
		respCh <- testResult.errorResponse
		testResult.numFailedRequests++
		return
	}
	respCh <- fmt.Sprintf("%.2f elapsed with response: %s response code: %v endpoint: %s", secs, string([]byte(body)), resp.StatusCode, endpoint)
	testResult.numSuccessfulRequests++
	testResult.totalRequests++
	testResult.Unlock()
}

func getHttpClient(skipVerify bool) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
	}
	client := &http.Client{Transport: tr}
	return client
}

func cleanup() {
	log.Println("Cleaning up")
	deleteFun()
	deleteNamespace()
}

func deleteFun() {
	stdoutStderr, err := deleteK8s(functionYaml)
	output := string(stdoutStderr)
	if err != nil {
		log.Fatal("unable to delete function ", functionName, ":\n", output)
	}
	timeout := time.After(10 * time.Minute)
	tick := time.Tick(1 * time.Second)
	for {
		select {
		case <-timeout:
			log.Fatal("timed out waiting for ", functionName, " pod to be deleted\n")
		case <-tick:
			cmd := exec.Command("kubectl", "-n", namespace, "get", "pod", "-l", "function="+functionName)
			stdoutStderr, err := cmd.CombinedOutput()
			if err == nil && strings.Contains(string(stdoutStderr), "no resources found") {
				return
			}
		}
	}
}

func deleteNamespace() {
	stdoutStderr, err := deleteK8s(nsYaml)
	output := string(stdoutStderr)
	if err != nil && !strings.Contains(output, "not found") {
		log.Fatal("unable to delete namespace ", namespace, ":\n", output)
	}
	timeout := time.After(10 * time.Minute)
	tick := time.Tick(1 * time.Second)
	for {
		cmd := exec.Command("kubectl", "get", "ns", namespace, "-oyaml")
		select {
		case <-timeout:
			stdoutStderr, err := cmd.CombinedOutput()
			if err != nil {
				log.Fatalf("unable to get ns: %v\n", string(stdoutStderr))
			}
			log.Printf("current state of the ns: %s is:\n %v", namespace, string(stdoutStderr))
			log.Fatal("timed out waiting for namespace: ", namespace, " to be deleted\n")
		case <-tick:
			stdoutStderr, err := cmd.CombinedOutput()
			if err != nil && strings.Contains(string(stdoutStderr), "NotFound") {
				return
			}
		}
	}
}

func deleteK8s(yamlFile string) (string, error) {
	cmd := exec.Command("kubectl", "delete", "-f", yamlFile, "-n", namespace, "--grace-period=0", "--force", "--ignore-not-found")
	stdoutStderr, err := cmd.CombinedOutput()
	output := string(stdoutStderr)
	return output, err
}

func checkFunctionHpa() ([]byte, error) {
	functionHpaCmd := exec.Command("kubectl", "-n", namespace, "get", "hpa", "-l", "function="+functionName, "-oyaml")
	hpaOutput, err := functionHpaCmd.CombinedOutput()
	return hpaOutput, err
}

func checkFunctionAutoscaled() {
	testResult.RLock()
	functionHpaCmd := exec.Command("kubectl", "-n", namespace, "get", "hpa", "-l", "function="+functionName, "-ojsonpath={.items[0].metadata.name} {.items[0].spec.minReplicas} {.items[0].status.currentReplicas} {.items[0].status.currentCPUUtilizationPercentage}")
	hpaOutput, err := functionHpaCmd.CombinedOutput()
	if err != nil {
		testResult.resultMessage = fmt.Sprintf("error in fetching function hpa: %v \n", err)
		log.Printf(testResult.resultMessage)
	} else {
		result := "Function autoscale failed"
		status := strings.Split(strings.TrimSpace(string(hpaOutput)), " ")
		minReplicas, err := strconv.Atoi(status[1])
		if err != nil {
			minReplicas = 0
		}
		minReplicasStatus := fmt.Sprintf("Minimum number of replicas: %v", minReplicas)
		currentReplicas, err := strconv.Atoi(status[2])
		if err != nil {
			currentReplicas = 0
		}
		currentReplicasStatus := fmt.Sprintf("Current number of replicas: %v", currentReplicas)

		cpuStatus := fmt.Sprintf("CPU utilization: %v%s", 0, "%")
		if len(status) == 4 {
			currentCPUUtilizationPercentage, err := strconv.Atoi(status[3])
			if err != nil {
				currentCPUUtilizationPercentage = 0
			}
			cpuStatus = fmt.Sprintf("CPU utilization: %v%s", currentCPUUtilizationPercentage, "%")
		}

		if currentReplicas > minReplicas {
			result = "Function autoscale succeeded"
		}
		finalStatus := fmt.Sprintf("Test HPA final status: %s \n%s \n%s \n%s\n", result, minReplicasStatus, currentReplicasStatus, cpuStatus)
		testResult.resultMessage = finalStatus

		if testResult.totalRequests > 0 {
			totalRequests := fmt.Sprintf("Toatl number of requests: %v \n", testResult.totalRequests)
			testResult.resultMessage = fmt.Sprintf("%s %s\n", testResult.resultMessage, strings.TrimSpace(totalRequests))
		}

		if testResult.numSuccessfulRequests > 0 {
			numSuccessfulRequests := fmt.Sprintf("Successful requests: %v \n", testResult.numSuccessfulRequests)
			testResult.resultMessage = fmt.Sprintf("%s %s\n", testResult.resultMessage, strings.TrimSpace(numSuccessfulRequests))
		}

		if testResult.numFailedRequests > 0 {
			numFailedRequests := fmt.Sprintf("Failed resquests: %v \n", testResult.numFailedRequests)
			testResult.resultMessage = fmt.Sprintf("%s %s\n", testResult.resultMessage, strings.TrimSpace(numFailedRequests))
			testResult.totalRequests = testResult.totalRequests + testResult.numFailedRequests
		}

		log.Println(testResult.resultMessage)
		testResult.RUnlock()
	}
}

func (slack *Slack) sendNotificationtoSlackChannel(testResult *TestResult) {
	textMessage := fmt.Sprintf(`{"channel": "%v", "text":"%v"}"`, slack.SlackChaneel, testResult.resultMessage)
	var jsonStr = []byte(textMessage)
	req, err := http.NewRequest("POST", slack.SlackEndpoint, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("unable to send slack notification to endpoint: %v : Error: %v", slack.SlackChaneel, err)
	}
	defer resp.Body.Close()
	log.Println("slack response status:", resp.Status)
	body, _ := ioutil.ReadAll(resp.Body)
	log.Println("slack response response body:", string(body))
}

func NewSlack() *Slack {
	sUrl := fmt.Sprintf("%s%s", slackEndpoint, os.Getenv("LOAD_TEST_SLACK_TOKEN"))
	if len(sUrl) == 0 {
		sUrl = slackEndpoint
	}
	sChannel := os.Getenv("LOAD_TEST_SLACK_CHANNEL")
	if len(sChannel) == 0 {
		sChannel = slackChaneel
	}
	s := &Slack{sUrl, sChannel}

	log.Printf("Slack: %v", s)
	return s
}

func NewTestResult() *TestResult {
	t := &TestResult{}
	return t
}
