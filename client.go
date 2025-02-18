package kratos

import (
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/goph/emperror"
	"github.com/xmidt-org/webpa-common/logging"
	"github.com/xmidt-org/wrp-go/v3"
)

// Client is what function calls we expose to the user of kratos
type Client interface {
	Hostname() string
	HandlerRegistry() HandlerRegistry
	Send(message *wrp.Message)
	Close() error
}

// sendWRPFunc is the function for sending a message downstream.
type sendWRPFunc func(*wrp.Message)

type client struct {
	deviceID        string
	userAgent       string
	deviceProtocols string
	hostname        string
	registry        HandlerRegistry
	handlePingMiss  HandlePingMiss
	encoderSender   encoderSender
	decoderSender   decoderSender
	connection      websocketConnection
	headerInfo      *clientHeader
	logger          log.Logger
	done            chan struct{}
	wg              sync.WaitGroup
	pingConfig      PingConfig
	once            sync.Once
}

// used to track everything that we want to know about the client headers
type clientHeader struct {
	deviceName   string
	firmwareName string
	modelName    string
	manufacturer string
	token        string
}

// websocketConnection maintains the websocket connection upstream (to XMiDT).
type websocketConnection interface {
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	Close() error
}

// Hostname provides the client's hostname.
func (c *client) Hostname() string {
	return c.hostname
}

// HandlerRegistry returns the HandlerRegistry that the client maintains.
func (c *client) HandlerRegistry() HandlerRegistry {
	return c.registry
}

// Send is used to open a channel for writing to XMiDT
func (c *client) Send(message *wrp.Message) {
	c.encoderSender.EncodeAndSend(message)
}

// Close closes connections downstream and the socket upstream.
func (c *client) Close() error {
	var connectionErr error
	c.once.Do(func() {
		logging.Info(c.logger).Log(logging.MessageKey(), "Closing client...")
		close(c.done)
		c.wg.Wait()
		c.decoderSender.Close()
		c.encoderSender.Close()
		connectionErr = c.connection.Close()
		c.connection = nil
		// TODO: if this fails, can we really do anything. Is there potential for leaks?
		// if err != nil {
		// 	return emperror.Wrap(err, "Failed to close connection")
		// }
		logging.Info(c.logger).Log(logging.MessageKey(), "Client Closed")
	})
	return connectionErr
}

// going to be used to access the HandleMessage() function
func (c *client) read() {
	defer c.wg.Done()
	logging.Info(c.logger).Log(logging.MessageKey(), "Watching socket for messages.")

	for {
		select {
		case <-c.done:
			logging.Info(c.logger).Log(logging.MessageKey(), "Stopped reading from socket.")
			return
		default:
			logging.Debug(c.logger).Log(logging.MessageKey(), "Reading message...")

			_, serverMessage, err := c.connection.ReadMessage()
			if err != nil {
				logging.Error(c.logger, emperror.Context(err)...).
					Log(logging.MessageKey(), "Failed to read message. Exiting out of read loop.", logging.ErrorKey(), err.Error())
				return
			}
			c.decoderSender.DecodeAndSend(serverMessage)

			logging.Debug(c.logger).Log(logging.MessageKey(), "Message sent to be decoded")
		}
	}
}
