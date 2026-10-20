package notification

import (
	"encoding/json"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/beat/backend/internal/model"
)

func renderPresentation(dispatch model.NotificationDispatch, config ProviderConfig) (model.NotificationPayload, error) {
	payload := dispatch.Payload
	data := make(map[string]string, len(payload.Data)+4)
	for key, value := range payload.Data {
		data[key] = value
	}
	data["subject"], data["text"], data["delivery.id"], data["delivery.attempt"] = payload.Subject, payload.Text, dispatch.DeliveryID, strconv.Itoa(dispatch.Attempt)
	var escape func(string) string
	if dispatch.ChannelType == TypeTelegram {
		if config.ParseMode == "MarkdownV2" {
			escape = EscapeMarkdownV2
		}
		if config.ParseMode == "HTML" {
			escape = html.EscapeString
		}
	}
	subject, err := renderMessagePart(dispatch.TemplateSubject, payload.Subject, TemplateValues{Data: data, Maximum: 200})
	if err != nil {
		return payload, err
	}
	text, err := renderMessagePart(dispatch.TemplateText, payload.Text, TemplateValues{Data: data, Maximum: 16 * 1024, Escape: escape})
	if err != nil {
		return payload, err
	}
	if strings.ContainsAny(subject, "\r\n") {
		return payload, ErrTemplate
	}
	payload.Subject, payload.Text, payload.Data = subject, text, data
	return payload, nil
}

func renderMessagePart(template, fallback string, values TemplateValues) (string, error) {
	if template == "" {
		if values.Escape != nil {
			fallback = values.Escape(fallback)
		}
		if len(fallback) > values.Maximum {
			return "", ErrTemplate
		}
		return fallback, nil
	}
	parts, err := CompileTemplate(template, false)
	if err != nil {
		return "", err
	}
	return RenderDeclarative(parts, values)
}

func customRequest(config ProviderConfig, payload model.NotificationPayload) (string, []byte, string, error) {
	endpoint, err := customEndpoint(config, payload)
	if err != nil {
		return "", nil, "", err
	}
	if config.Method == "GET" {
		if len(config.Body) > 0 && string(config.Body) != "null" {
			return "", nil, "", ErrProviderConfig
		}
		return endpoint.String(), nil, "application/json", nil
	}
	if len(config.Body) == 0 {
		return endpoint.String(), nil, "application/json", nil
	}
	var body any
	if err := json.Unmarshal(config.Body, &body); err != nil {
		return "", nil, "", ErrTemplate
	}
	budget, byteBudget := maximumJSONNodes, maximumRequestBytes
	values := TemplateValues{Data: payload.Data, Secrets: config.NamedSecrets, Maximum: byteBudget}
	rendered, err := renderJSONLeaves(body, leafRender{values: values, budget: &budget, bytes: &byteBudget})
	if err != nil {
		return "", nil, "", err
	}
	encoded, contentType, err := encodeCustomBody(config.BodyMode, rendered)
	if err != nil {
		return "", nil, "", err
	}
	return endpoint.String(), encoded, contentType, nil
}

func encodeCustomBody(mode string, rendered any) ([]byte, string, error) {
	switch mode {
	case "json":
		encoded, err := json.Marshal(rendered)
		return encoded, "application/json", err
	case "text":
		text, ok := rendered.(string)
		if !ok {
			return nil, "", ErrTemplate
		}
		return []byte(text), "text/plain; charset=utf-8", nil
	case "form":
		fields, ok := rendered.(map[string]any)
		if !ok {
			return nil, "", ErrTemplate
		}
		form := url.Values{}
		for name, value := range fields {
			text, ok := value.(string)
			if !ok {
				return nil, "", ErrTemplate
			}
			form.Set(name, text)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", ErrProviderConfig
	}
}

func customEndpoint(config ProviderConfig, payload model.NotificationPayload) (*url.URL, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil {
		return nil, ErrProviderConfig
	}
	values := TemplateValues{Data: payload.Data, Secrets: config.NamedSecrets, Maximum: maximumEndpointBytes}
	query := endpoint.Query()
	for name, template := range config.Query {
		rendered, err := renderCustomValue(template, values)
		if err != nil {
			return nil, err
		}
		query.Set(name, rendered)
	}
	endpoint.RawQuery = query.Encode()
	if len(endpoint.String()) > maximumEndpointBytes {
		return nil, ErrTemplate
	}
	return endpoint, nil
}

func renderCustomValue(value string, values TemplateValues) (string, error) {
	parts, err := CompileTemplate(value, true)
	if err != nil {
		return "", err
	}
	return RenderDeclarative(parts, values)
}

type leafRender struct {
	values TemplateValues
	depth  int
	budget *int
	bytes  *int
}

func renderJSONLeaves(value any, render leafRender) (any, error) {
	*render.budget--
	if *render.budget < 0 || render.depth > maximumJSONDepth {
		return nil, ErrTemplate
	}
	render.depth++
	switch typed := value.(type) {
	case string:
		return renderStringLeaf(typed, render)
	case map[string]any:
		result := map[string]any{}
		for key, child := range typed {
			if strings.Contains(key, "{{") || strings.Contains(key, "}}") {
				return nil, ErrTemplate
			}
			item, err := renderJSONLeaves(child, render)
			if err != nil {
				return nil, err
			}
			result[key] = item
		}
		return result, nil
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			item, err := renderJSONLeaves(child, render)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, nil
	default:
		return value, nil
	}
}

func renderStringLeaf(value string, render leafRender) (string, error) {
	if render.bytes != nil {
		render.values.Maximum = min(render.values.Maximum, *render.bytes)
	}
	rendered, err := renderCustomValue(value, render.values)
	if err != nil {
		return "", err
	}
	if render.bytes != nil {
		*render.bytes -= len(rendered)
	}
	return rendered, nil
}
