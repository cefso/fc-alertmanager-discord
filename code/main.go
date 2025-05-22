package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"flag"
	"os"
	"bytes"

	"github.com/aliyun/fc-runtime-go-sdk/fc"
)

// Discord color values
const (
	ColorRed   = 0x992D22
	ColorGreen = 0x2ECC71
	ColorGrey  = 0x95A5A6
)

var (
	whURL = flag.String("webhook.url", os.Getenv("DISCORD_WEBHOOK"), "Discord WebHook URL.")
)

// HTTPTriggerEvent HTTP Trigger Request Event
type HTTPTriggerEvent struct {
	Version         *string           `json:"version"`
	RawPath         *string           `json:"rawPath"`
	Headers         map[string]string `json:"headers"`
	QueryParameters map[string]string `json:"queryParameters"`
	Body            *string           `json:"body"`
	IsBase64Encoded *bool             `json:"isBase64Encoded"`
	RequestContext  *struct {
		AccountId    string `json:"accountId"`
		DomainName   string `json:"domainName"`
		DomainPrefix string `json:"domainPrefix"`
		RequestId    string `json:"requestId"`
		Time         string `json:"time"`
		TimeEpoch    string `json:"timeEpoch"`
		Http         struct {
			Method    string `json:"method"`
			Path      string `json:"path"`
			Protocol  string `json:"protocol"`
			SourceIp  string `json:"sourceIp"`
			UserAgent string `json:"userAgent"`
		} `json:"http"`
	} `json:"requestContext"`
}

func (h HTTPTriggerEvent) String() string {
	jsonBytes, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

// HTTPTriggerResponse HTTP Trigger Response struct
type HTTPTriggerResponse struct {
	StatusCode      int               `json:"statusCode"`
	Headers         map[string]string `json:"headers,omitempty"`
	IsBase64Encoded bool              `json:"isBase64Encoded,omitempty"`
	Body            string            `json:"body"`
}

func NewHTTPTriggerResponse(statusCode int) *HTTPTriggerResponse {
	return &HTTPTriggerResponse{StatusCode: statusCode}
}

func (h *HTTPTriggerResponse) String() string {
	jsonBytes, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

func (h *HTTPTriggerResponse) WithStatusCode(statusCode int) *HTTPTriggerResponse {
	h.StatusCode = statusCode
	return h
}

func (h *HTTPTriggerResponse) WithHeaders(headers map[string]string) *HTTPTriggerResponse {
	h.Headers = headers
	return h
}

func (h *HTTPTriggerResponse) WithIsBase64Encoded(isBase64Encoded bool) *HTTPTriggerResponse {
	h.IsBase64Encoded = isBase64Encoded
	return h
}

func (h *HTTPTriggerResponse) WithBody(body string) *HTTPTriggerResponse {
	h.Body = body
	return h
}

func HandleRequest(event HTTPTriggerEvent) (*HTTPTriggerResponse, error) {
	fmt.Printf("event: %v\n", event)
	if event.Body == nil {
		return NewHTTPTriggerResponse(http.StatusBadRequest).
			WithBody(fmt.Sprintf("the request did not come from an HTTP Trigger, event: %v", event)), nil
	}

	reqBody := *event.Body
	if event.IsBase64Encoded != nil && *event.IsBase64Encoded {
		decodedByte, err := base64.StdEncoding.DecodeString(*event.Body)
		if err != nil {
			return NewHTTPTriggerResponse(http.StatusBadRequest).
				WithBody(fmt.Sprintf("HTTP Trigger body is not base64 encoded, err: %v", err)), nil
		}
		reqBody = string(decodedByte)
	}
	var amo *alertManOut
	err := json.Unmarshal([]byte(reqBody), &amo)
	sendWebhook(amo)
	if err != nil {
		panic(fmt.Errorf("JSON解析失败: %v", err))
	}
	return NewHTTPTriggerResponse(http.StatusOK).WithBody(reqBody), nil
}

/*
// GetRawRequestEvent: obtain the raw request event
func GetRawRequestEvent(event []byte) (*HTTPTriggerResponse, error) {
	fmt.Printf("raw event: %s\n", string(event))
	return NewHTTPTriggerResponse(http.StatusOK).WithBody(string(event)), nil
}
*/

//定义alertmanager数据格式
type alertManAlert struct {
	Annotations struct {
		Description string `json:"description"`
		Summary     string `json:"summary"`
	} `json:"annotations"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Labels       map[string]string `json:"labels"`
	StartsAt     string            `json:"startsAt"`
	Status       string            `json:"status"`
}

type alertManOut struct {
	Alerts            []alertManAlert `json:"alerts"`
	CommonAnnotations struct {
		Summary string `json:"summary"`
	} `json:"commonAnnotations"`
	CommonLabels struct {
		Alertname string `json:"alertname"`
	} `json:"commonLabels"`
	ExternalURL string `json:"externalURL"`
	GroupKey    string `json:"groupKey"`
	GroupLabels struct {
		Alertname string `json:"alertname"`
	} `json:"groupLabels"`
	Receiver string `json:"receiver"`
	Status   string `json:"status"`
	Version  string `json:"version"`
}

//定义discord数据格式
type discordOut struct {
	Content string         `json:"content"`
	Embeds  []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields"`
}

type discordEmbedField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func sendWebhook(amo *alertManOut) {
	groupedAlerts := make(map[string][]alertManAlert)

	for _, alert := range amo.Alerts {
		groupedAlerts[alert.Status] = append(groupedAlerts[alert.Status], alert)
	}

	for status, alerts := range groupedAlerts {
		DO := discordOut{}

		RichEmbed := discordEmbed{
			Title:       fmt.Sprintf("[%s:%d] %s", strings.ToUpper(status), len(alerts), amo.CommonLabels.Alertname),
			Description: amo.CommonAnnotations.Summary,
			Color:       ColorGrey,
			Fields:      []discordEmbedField{},
		}

		if status == "firing" {
			RichEmbed.Color = ColorRed
		} else if status == "resolved" {
			RichEmbed.Color = ColorGreen
		}

		if amo.CommonAnnotations.Summary != "" {
			DO.Content = fmt.Sprintf(" === %s === \n", amo.CommonAnnotations.Summary)
		}

		for _, alert := range alerts {
			realname := alert.Labels["instance"]
			if strings.Contains(realname, "localhost") && alert.Labels["exported_instance"] != "" {
				realname = alert.Labels["exported_instance"]
			}

			RichEmbed.Fields = append(RichEmbed.Fields, discordEmbedField{
				Name:  fmt.Sprintf("[%s]: %s on %s", strings.ToUpper(status), alert.Labels["alertname"], realname),
				Value: alert.Annotations.Description,
			})
		}

		DO.Embeds = []discordEmbed{RichEmbed}

		DOD, _ := json.Marshal(DO)
		http.Post(*whURL, "application/json", bytes.NewReader(DOD))
	}
}

func main() {
	fc.Start(HandleRequest)
}
