package mqtt

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/project-flotta/flotta-operator/internal/edgeapi"
	"go.uber.org/zap"
)

var Config edgeapi.Config

var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("TOPIC: %s\n", msg.Topic())
	fmt.Printf("MSG: %s\n", msg.Payload())
}

func brokerConnect(logger *zap.SugaredLogger) (error, mqtt.Client) {
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

	// opts := mqtt.NewClientOptions().AddBroker("tcp://mosquitto.default.svc.cluster.local:1883").SetClientID(string(client_id))
	opts := mqtt.NewClientOptions().AddBroker("tcp://localhost:1883").SetClientID(string(client_id))

	opts.SetKeepAlive(60 * time.Second)
	// Set the message callback handler
	opts.SetDefaultPublishHandler(f)
	opts.SetPingTimeout(1 * time.Second)
	opts.AutoReconnect = true

	c := mqtt.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		// panic(token.Error())
		logger.Errorf("Cannot create registration client certificate: %w", token.Error())
		return token.Error(), nil
	}

	return nil, c
}

func subscribeTopic(topic string, c mqtt.Client, logger *zap.SugaredLogger) (error, bool) {
	// Subscribe to a topic
	if token := c.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		logger.Errorf("Error subscribing to topic: %w", token.Error())
		// os.Exit(1)
		return nil, false
	}

	return nil, true
}

func publishTopic(c mqtt.Client, logger *zap.SugaredLogger) error {
	token := c.Publish("device/data/1/in", 0, false, "Hello World")
	token.Wait()
	time.Sleep(6 * time.Second)
	if token.Error() != nil {
		logger.Errorf("Failed to publish to topic: %w", token.Error())
		return token.Error()
	}
	return nil
}
