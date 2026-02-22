package converter

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"kiro-go-gw/internal/models"
)

func BuildKiroPayload(req *models.ChatCompletionRequest, conversationID, profileArn string) (map[string]interface{}, error) {
	systemPrompt, _ := convertMessages(req.Messages)
	tools := convertTools(req.Tools)

	// Find the last user message for currentMessage
	var lastUserMsgIndex int = -1
	var lastNonSystemIndex int = -1

	// Find the last non-system message index
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role != "system" {
			lastNonSystemIndex = i
			break
		}
	}

	// Find the last user message for currentMessage
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUserMsgIndex = i
			break
		}
	}

	// If no user message found or no content blocks, error
	if lastUserMsgIndex < 0 {
		return nil, fmt.Errorf("no user message found in request")
	}

	// Validate that the last non-system message is not a tool message
	// Tool results cannot be represented as userInputMessage
	if lastNonSystemIndex >= 0 && lastUserMsgIndex != lastNonSystemIndex {
		lastMsgRole := req.Messages[lastNonSystemIndex].Role
		if lastMsgRole == "tool" {
			return nil, fmt.Errorf("last message cannot be tool result, expected user or assistant message")
		}
	}

	// Build history from all messages EXCEPT the last user message
	var history []map[string]interface{}
	if lastUserMsgIndex > 0 {
		_, history = convertMessages(req.Messages[:lastUserMsgIndex])
	}
	if lastUserMsgIndex < len(req.Messages)-1 {
		_, afterUserMsgs := convertMessages(req.Messages[lastUserMsgIndex+1:])
		history = append(history, afterUserMsgs...)
	}

	// Extract text content and images from the last user message
	userTextContent := extractTextContent(req.Messages[lastUserMsgIndex].Content)
	userImages := extractImages(req.Messages[lastUserMsgIndex].Content)

	// Get envState
	envState := getEnvState()

	// Build userInputMessageContext with envState
	userInputMessageContext := map[string]interface{}{
		"envState": envState,
	}

	// Add tools to userInputMessageContext
	if tools != nil {
		userInputMessageContext["tools"] = tools
	}

	// Build userInputMessage (following Kiro format)
	userInputMessage := map[string]interface{}{
		"content":                 userTextContent,
		"modelId":                 normalizeModelName(req.Model),
		"origin":                  "KIRO_CLI",
		"userInputMessageContext": userInputMessageContext,
	}

	// Add images if present (separate field)
	if len(userImages) > 0 {
		userInputMessage["images"] = userImages
	}

	// Build conversationState structure (Kiro's native format)
	conversationState := map[string]interface{}{
		"chatTriggerType": "MANUAL",
		"conversationId":  conversationID,
		"currentMessage": map[string]interface{}{
			"userInputMessage": userInputMessage,
		},
	}

	// Add history if there are previous messages
	if len(history) > 0 {
		conversationState["history"] = history
	}

	// Build payload
	payload := map[string]interface{}{
		"conversationState": conversationState,
	}

	// Add profileArn only if provided (not needed for AWS SSO)
	if profileArn != "" {
		payload["profileArn"] = profileArn
	}

	if systemPrompt != "" {
		payload["systemPrompt"] = systemPrompt
	}

	if req.MaxTokens > 0 {
		payload["maxTokens"] = req.MaxTokens
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	if req.TopP > 0 {
		payload["topP"] = req.TopP
	}

	if req.Stream {
		payload["enableStreaming"] = true
	}

	return payload, nil
}

func getEnvState() map[string]interface{} {
	osName := "linux"
	cwd := "/home/user"
	if IsExecEnv() {
		if actualCwd, err := os.Getwd(); err == nil {
			cwd = actualCwd
		}
	}
	return map[string]interface{}{
		"operatingSystem":         osName,
		"currentWorkingDirectory": cwd,
	}
}

func IsExecEnv() bool {
	return true
}

func generateAgentContinuationId() string {
	// Generate a UUID-like string for agentContinuationId
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		randHex(8), randHex(4), randHex(4), randHex(4), randHex(12))
}

func randHex(n int) string {
	hexChars := "0123456789abcdef"
	result := make([]byte, n)
	if _, err := rand.Read(result); err != nil {
		return ""
	}
	for i := 0; i < n; i++ {
		result[i] = hexChars[result[i]%16]
	}
	return string(result)
}

func normalizeModelName(name string) string {
	// Convert dash to dot for minor version
	// claude-haiku-4-5 -> claude-haiku-4.5
	name = strings.ReplaceAll(name, "-4-5", "-4.5")
	name = strings.ReplaceAll(name, "-4-0", "-4.0")
	name = strings.ReplaceAll(name, "-3-5", "-3.5")
	name = strings.ReplaceAll(name, "-3-0", "-3.0")

	// Strip date suffix or "latest" suffix
	// claude-haiku-4-5-20251001 -> claude-haiku-4.5
	// claude-haiku-4-5-latest -> claude-haiku-4.5
	parts := strings.Split(name, "-")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		isDate := len(last) == 8
		if isDate {
			for _, c := range last {
				if c < '0' || c > '9' {
					isDate = false
					break
				}
			}
		}

		if isDate || last == "latest" {
			parts = parts[:len(parts)-1]
			// Rejoin and fix the version
			if len(parts) > 0 {
				lastPart := parts[len(parts)-1]
				if len(lastPart) == 1 {
					parts[len(parts)-1] = lastPart + ".0"
				}
			}
			name = strings.Join(parts, "-")
		}
	}

	return name
}

func convertMessages(msgs []models.ChatMessage) (string, []map[string]interface{}) {
	var systemPrompt strings.Builder
	var converted []map[string]interface{}
	envState := getEnvState()

	for _, msg := range msgs {
		if msg.Role == "system" {
			content := extractTextContent(msg.Content)
			if content != "" {
				if systemPrompt.Len() > 0 {
					systemPrompt.WriteString("\n")
				}
				systemPrompt.WriteString(content)
			}
			continue
		}

		// Build Kiro format: userInputMessage or assistantResponseMessage
		var convertedMsg map[string]interface{}
		content := extractTextContent(msg.Content)

		if msg.Role == "user" || msg.Role == "tool" {
			// Both user and tool messages become userInputMessage in Kiro format
			// Tool results are embedded in userInputMessageContext
			userInputMsg := map[string]interface{}{
				"content": content,
				"origin":  "KIRO_CLI",
			}

			// Add envState
			userInputMsgContext := map[string]interface{}{
				"envState": envState,
			}

			// Handle tool results (for both tool role messages and user messages with tool results)
			toolResults := buildToolResults(msg)
			if len(toolResults) > 0 {
				userInputMsgContext["toolResults"] = toolResults
			}

			userInputMsg["userInputMessageContext"] = userInputMsgContext

			// Handle images in user message
			images := extractImages(msg.Content)
			if len(images) > 0 {
				userInputMsg["images"] = images
			}

			convertedMsg = map[string]interface{}{
				"userInputMessage": userInputMsg,
			}
		} else if msg.Role == "assistant" {
			assistantMsg := map[string]interface{}{
				"content": content,
			}

			// Add messageId if available
			if msg.Name != "" {
				assistantMsg["messageId"] = msg.Name
			}

			// Handle tool calls
			if len(msg.ToolCalls) > 0 {
				toolUses := make([]map[string]interface{}, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					toolUses = append(toolUses, map[string]interface{}{
						"toolUseId": tc.ID,
						"name":      tc.Function.Name,
						"input":     parseArguments(tc.Function.Arguments),
					})
				}
				assistantMsg["toolUses"] = toolUses
			}

			convertedMsg = map[string]interface{}{
				"assistantResponseMessage": assistantMsg,
			}
		}

		if convertedMsg != nil {
			converted = append(converted, convertedMsg)
		}
	}

	return systemPrompt.String(), converted
}

func buildToolResults(msg models.ChatMessage) []map[string]interface{} {
	var results []map[string]interface{}

	// Handle tool results from tool role messages
	if msg.Role == "tool" {
		toolUseID := msg.ToolCallID
		if toolUseID == "" {
			toolUseID = msg.Name
		}
		content := extractTextContent(msg.Content)
		results = append(results, map[string]interface{}{
			"toolUseId": toolUseID,
			"content": []map[string]interface{}{
				{"text": content},
			},
			"status": "success",
		})
	}

	return results
}

func convertTools(tools []models.Tool) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}

		var name, description string
		var inputSchema interface{}

		// Standard OpenAI format (function field)
		if tool.Function != nil {
			name = tool.Function.Name
			description = tool.Function.Description
			inputSchema = tool.Function.Parameters
		} else {
			// Flat format (Cursor-style)
			name = tool.Name
			description = tool.Description
			inputSchema = tool.InputSchema
		}

		if name == "" {
			continue
		}

		// Wrap inputSchema in {"json": ...} as per Kiro format
		wrappedSchema := map[string]interface{}{
			"json": inputSchema,
		}

		result = append(result, map[string]interface{}{
			"toolSpecification": map[string]interface{}{
				"name":        name,
				"description": description,
				"inputSchema": wrappedSchema,
			},
		})
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func extractTextContent(content interface{}) string {
	if content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var text strings.Builder
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if t, ok := block["text"].(string); ok {
					text.WriteString(t)
				}
			}
		}
		return text.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func extractContentBlocks(content interface{}) []map[string]interface{} {
	if content == nil {
		return nil
	}

	var blocks []map[string]interface{}

	switch v := content.(type) {
	case string:
		if v != "" {
			blocks = append(blocks, map[string]interface{}{
				"type": "text",
				"text": v,
			})
		}
	case []interface{}:
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				blockType, _ := block["type"].(string)
				if blockType == "text" {
					if text, ok := block["text"].(string); ok && text != "" {
						blocks = append(blocks, map[string]interface{}{
							"type": "text",
							"text": text,
						})
					}
				} else if blockType == "image_url" {
					if imgURL, ok := block["image_url"].(map[string]interface{}); ok {
						if url, ok := imgURL["url"].(string); ok {
							if strings.HasPrefix(url, "data:image") {
								blocks = append(blocks, map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type":      "base64",
										"mediaType": extractMediaType(url),
										"data":      extractBase64Data(url),
									},
								})
							} else {
								blocks = append(blocks, map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type": "url",
										"url":  url,
									},
								})
							}
						}
					}
				}
			}
		}
	}

	return blocks
}

func extractImages(content interface{}) []map[string]interface{} {
	if content == nil {
		return nil
	}

	var images []map[string]interface{}

	switch v := content.(type) {
	case []interface{}:
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if block["type"] == "image_url" {
					if imgURL, ok := block["image_url"].(map[string]interface{}); ok {
						if url, ok := imgURL["url"].(string); ok {
							// Handle data URL (already base64 encoded)
							if strings.HasPrefix(url, "data:image") {
								mediaType := extractMediaType(url)
								format := strings.TrimPrefix(mediaType, "image/")
								images = append(images, map[string]interface{}{
									"format": format,
									"source": map[string]interface{}{
										"bytes": extractBase64Data(url),
									},
								})
							} else {
								// Try to fetch URL and convert to base64
								if data, err := fetchImageAsBase64(url); err == nil {
									format := detectImageFormat(data)
									images = append(images, map[string]interface{}{
										"format": format,
										"source": map[string]interface{}{
											"bytes": data,
										},
									})
								} else {
									// Fallback: include as URL reference (legacy format)
									images = append(images, map[string]interface{}{
										"format": "png",
										"source": map[string]interface{}{
											"url": url,
										},
									})
								}
							}
						}
					}
				}
			}
		}
	}

	return images
}

func fetchImageAsBase64(imageURL string) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch image: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

func detectImageFormat(data string) string {
	// Simple format detection based on base64 header
	decoded, err := base64.StdEncoding.DecodeString(data[:100])
	if err != nil {
		return "png"
	}
	if len(decoded) >= 4 {
		if decoded[0] == 0xFF && decoded[1] == 0xD8 {
			return "jpeg"
		}
		if decoded[0] == 0x89 && string(decoded[:4]) == "PNG" {
			return "png"
		}
		if decoded[0] == 0x47 && decoded[1] == 0x49 {
			return "gif"
		}
		if decoded[0] == 0x52 && string(decoded[:4]) == "RIFF" {
			return "webp"
		}
	}
	return "png"
}

func extractMediaType(dataURL string) string {
	parts := strings.Split(dataURL, ";")
	if len(parts) > 1 {
		return strings.TrimPrefix(parts[0], "data:")
	}
	return "image/png"
}

func extractBase64Data(dataURL string) string {
	parts := strings.Split(dataURL, ",")
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func parseArguments(args string) map[string]interface{} {
	if args == "" {
		return make(map[string]interface{})
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return make(map[string]interface{})
	}

	return result
}
