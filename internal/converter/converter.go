package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"kiro-go-gw/internal/models"
)

func BuildKiroPayload(req *models.ChatCompletionRequest, conversationID, profileArn string) (map[string]interface{}, error) {
	systemPrompt, _ := convertMessages(req.Messages)
	tools := convertTools(req.Tools)

	// Find the last user message for currentMessage
	var userContentBlocks []map[string]interface{}
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
			userContentBlocks = extractContentBlocks(req.Messages[i].Content)
			lastUserMsgIndex = i
			break
		}
	}

	// If no user message found or no content blocks, error
	if len(userContentBlocks) == 0 {
		return nil, fmt.Errorf("no user message found in request")
	}

	// Validate that the last non-system message is not a tool message
	// Tool results cannot be represented as userInputMessage
	// Allow last message to be user or assistant (for continuing conversations)
	if lastNonSystemIndex >= 0 && lastUserMsgIndex != lastNonSystemIndex {
		lastMsgRole := req.Messages[lastNonSystemIndex].Role
		if lastMsgRole == "tool" {
			return nil, fmt.Errorf("last message cannot be tool result, expected user or assistant message")
		}
	}

	// Build history from all messages EXCEPT the last user message
	// (includes previous user/assistant exchanges and any assistant messages after the last user)
	var history []map[string]interface{}
	if lastUserMsgIndex < len(req.Messages)-1 {
		// There are messages after the last user message (e.g., assistant response with tool_calls)
		// Convert messages before the last user message
		_, history = convertMessages(req.Messages[:lastUserMsgIndex])
		// Also add messages after the last user message to history
		if lastUserMsgIndex+1 < len(req.Messages) {
			_, afterUserMsgs := convertMessages(req.Messages[lastUserMsgIndex+1:])
			history = append(history, afterUserMsgs...)
		}
	} else if lastUserMsgIndex > 0 {
		// Last user message is not at the end, get messages before it
		_, history = convertMessages(req.Messages[:lastUserMsgIndex])
	}

	// Extract text content and images from the last user message
	userTextContent := extractTextContent(req.Messages[lastUserMsgIndex].Content)
	userImages := extractImages(req.Messages[lastUserMsgIndex].Content)

	// Build userInputMessage (following Python converter format)
	userInputMessage := map[string]interface{}{
		"content": userTextContent,
		"modelId": normalizeModelName(req.Model),
		"origin":  "AI_EDITOR",
	}

	// Add images if present (separate field)
	if len(userImages) > 0 {
		userInputMessage["images"] = userImages
	}

	// Add tools to userInputMessageContext
	if tools != nil {
		userInputMessage["userInputMessageContext"] = map[string]interface{}{
			"tools": tools,
		}
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

	// Tools are now in userInputMessageContext, not at top level

	if req.MaxTokens > 0 {
		payload["maxTokens"] = req.MaxTokens
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	return payload, nil
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

		convertedMsg := map[string]interface{}{
			"role": msg.Role,
		}

		// Handle content
		content := extractTextContent(msg.Content)
		if content != "" {
			convertedMsg["content"] = []map[string]interface{}{
				{"type": "text", "text": content},
			}
		} else {
			convertedMsg["content"] = []map[string]interface{}{}
		}

		// Handle tool calls (assistant message with tool_calls)
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, map[string]interface{}{
					"type":  "toolUse",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": parseArguments(tc.Function.Arguments),
				})
			}
			convertedMsg["toolCalls"] = toolCalls
		}

		// Handle tool results (tool role)
		if msg.Role == "tool" {
			toolUseID := msg.ToolCallID
			if toolUseID == "" {
				toolUseID = msg.Name // Fallback to name
			}
			convertedMsg["toolUseId"] = toolUseID
			convertedMsg["content"] = []map[string]interface{}{
				{"type": "toolResult", "toolUseId": toolUseID, "content": content},
			}
		}

		// Handle images in content
		images := extractImages(msg.Content)
		if len(images) > 0 {
			contentBlocks := convertedMsg["content"].([]map[string]interface{})
			contentBlocks = append(contentBlocks, images...)
			convertedMsg["content"] = contentBlocks
		}

		converted = append(converted, convertedMsg)
	}

	return systemPrompt.String(), converted
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

		result = append(result, map[string]interface{}{
			"toolSpec": map[string]interface{}{
				"name":        name,
				"description": description,
				"inputSchema": inputSchema,
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
							// Handle data URL
							if strings.HasPrefix(url, "data:image") {
								images = append(images, map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type":      "base64",
										"mediaType": extractMediaType(url),
										"data":      extractBase64Data(url),
									},
								})
							} else {
								images = append(images, map[string]interface{}{
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

	return images
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
