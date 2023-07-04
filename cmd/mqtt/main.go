package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	obv1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	managementv1alpha1 "github.com/project-flotta/flotta-operator/api/v1alpha1"
	"github.com/project-flotta/flotta-operator/internal/edgeapi"
)

const (
	initialDeviceNamespace = "default"
)

var (
	operatorNamespace = "flotta"
	scheme            = runtime.NewScheme()
)

type Message struct {
	FlottaDeviceID    string `json:"flotta_device_id"`
	Event             string `json:"event"`
	TenantID          string `json:"tenantId"`
	TenantName        string `json:"tenantName"`
	ApplicationID     string `json:"applicationId"`
	ApplicationName   string `json:"applicationName"`
	DeviceProfileID   string `json:"deviceProfileId"`
	DeviceProfileName string `json:"deviceProfileName"`
	DeviceName        string `json:"deviceName"`
	DevEUI            string `json:"devEui"`
	DevAddr           string `json:"devAddr"`
	Data              string `json:"data"`
	Confirmed         bool   `json:"confirmed"`
	Latitude          string `json:"latitude"`
	Longitude         string `json:"longitude"`
	Bandwidth         int    `json:"bandwidth"`
	Frequency         int    `json:"frequency"`
	SpreadingFactor   int    `json:"spreadingFactor"`
	CodeRate          string `json:"codeRate"`
	DeviceType        string `json:"device_type"`
	Time              string `json:"time"`
	RegionName        string `json:"region_name"`
	Tags              string `json:"tags"`
	LocationSource    string `json:"location_source"`
}

var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("TOPIC: %s\n", msg.Topic())

	if msg.Topic() == "device/up" {
		fmt.Printf("MSG: %s\n", msg.Payload())
		payload := string(msg.Payload())
		processMqttData(payload)
	}
}

var Config edgeapi.Config

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(managementv1alpha1.AddToScheme(scheme))
	utilruntime.Must(obv1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
}

func main() {

	go func() {
		MqttHandler()
	}()
	select {}

}

func logger(logLevel string) (error, *zap.SugaredLogger) {
	var level zapcore.Level
	err := level.UnmarshalText([]byte(logLevel))
	if err != nil {
		return err, nil
	}
	logConfig := zap.NewDevelopmentConfig()
	logConfig.Level.SetLevel(level)
	log, err := logConfig.Build()
	if err != nil {
		return err, nil
	}
	return nil, log.Sugar()
}

func MqttHandler() {

	rand.Seed(time.Now().Unix())

	str := "ThisIsaRandomString1234567890qwertyuiopasdfghjklzxcvbnmQWERTYUIOPASDFGJHJKLZCM"

	shuff := []rune(str)

	// Shuffling the string
	rand.Shuffle(len(shuff), func(i, j int) {
		shuff[i], shuff[j] = shuff[j], shuff[i]
	})
	client_id := string(shuff)

	mqtt.DEBUG = log.New(os.Stdout, "", 0)
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().AddBroker("tcp://localhost:1883").SetClientID(client_id)

	opts.SetKeepAlive(60 * time.Second)
	// Set the message callback handler
	opts.SetDefaultPublishHandler(f)
	opts.SetPingTimeout(1 * time.Second)

	c := mqtt.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	// Subscribe to a topic
	if token := c.Subscribe("device/up", 0, nil); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	// Publish a message
	token := c.Publish("testtopic/1", 0, false, "Hello World")
	token.Wait()

	time.Sleep(6 * time.Second)

	// // Unscribe
	// if token := c.Unsubscribe("testtopic/#"); token.Wait() && token.Error() != nil {
	// 	fmt.Println(token.Error())
	// 	os.Exit(1)
	// }

	// // Disconnect
	// c.Disconnect(250)
	// time.Sleep(1 * time.Second)
}

func processMqttData(payload string) {

	// Remove potential leading/trailing whitespaces or newlines
	payload = strings.TrimSpace(payload)

	var message Message
	err := json.Unmarshal([]byte(payload), &message)
	if err != nil {
		log.Printf("Failed to unmarshal JSON: %s", err)
		return
	}

	// Access the parsed values
	fmt.Printf("Received message:\n%+v\n", message.ApplicationName)

}

// Helper function to remove single quotes from the input string
func replaceQuotes(input string) string {
	return fmt.Sprintf(`"%s"`, input[1:len(input)-1])
}
