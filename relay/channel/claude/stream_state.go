package claude

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// Backport-of: 0ed497f066a68613375124303ef54f220267b334
// ClaudeToChatStreamState translates Anthropic content block indexes into the
// independent, dense index space used by Chat Completions tool_calls. Text and
// thinking blocks therefore do not create holes in the downstream tool array.
type ClaudeToChatStreamState struct {
	toolIndexByContentBlock map[int]int
	blockTypeByContentBlock map[int]string
	nextToolIndex           int
}

func NewClaudeToChatStreamState() *ClaudeToChatStreamState {
	return &ClaudeToChatStreamState{
		toolIndexByContentBlock: make(map[int]int),
		blockTypeByContentBlock: make(map[int]string),
	}
}

func (s *ClaudeToChatStreamState) ConvertChunk(claudeResponse *dto.ClaudeResponse) (*dto.ChatCompletionsStreamResponse, error) {
	if s == nil {
		return nil, fmt.Errorf("Claude-to-Chat stream state is required")
	}
	if claudeResponse == nil {
		return nil, nil
	}

	converted := *claudeResponse
	switch claudeResponse.Type {
	case "content_block_start":
		if claudeResponse.ContentBlock == nil {
			break
		}
		blockType := strings.TrimSpace(claudeResponse.ContentBlock.Type)
		if blockType == "" {
			break
		}
		if claudeResponse.Index == nil {
			return nil, fmt.Errorf("Claude content block stream start is missing index")
		}
		contentBlockIndex := *claudeResponse.Index
		s.blockTypeByContentBlock[contentBlockIndex] = blockType
		if blockType != "tool_use" {
			if isClaudeHostedToolStreamBlock(blockType) {
				return nil, nil
			}
			break
		}
		toolIndex, exists := s.toolIndexByContentBlock[contentBlockIndex]
		if !exists {
			toolIndex = s.nextToolIndex
			s.nextToolIndex++
			s.toolIndexByContentBlock[contentBlockIndex] = toolIndex
		}
		converted.Index = common.GetPointer(toolIndex)
	case "content_block_delta":
		if claudeResponse.Delta == nil || claudeResponse.Delta.Type != "input_json_delta" {
			break
		}
		if claudeResponse.Index == nil {
			return nil, fmt.Errorf("Claude tool-use stream delta is missing content block index")
		}
		if claudeResponse.Delta.PartialJson == nil {
			return nil, fmt.Errorf("Claude tool-use stream delta is missing partial JSON")
		}
		contentBlockIndex := *claudeResponse.Index
		toolIndex, exists := s.toolIndexByContentBlock[contentBlockIndex]
		if !exists {
			if isClaudeHostedToolStreamBlock(s.blockTypeByContentBlock[contentBlockIndex]) {
				return nil, nil
			}
			return nil, fmt.Errorf("Claude tool-use stream delta references unknown content block index %d", contentBlockIndex)
		}
		converted.Index = common.GetPointer(toolIndex)
	case "content_block_stop":
		if claudeResponse.Index != nil {
			delete(s.blockTypeByContentBlock, *claudeResponse.Index)
		}
	}

	return StreamResponseClaude2OpenAI(&converted), nil
}

func isClaudeHostedToolStreamBlock(blockType string) bool {
	switch blockType {
	case "server_tool_use", "mcp_tool_use", "web_search_tool_result", "mcp_tool_result", "code_execution_tool_result", "web_fetch_tool_result":
		return true
	default:
		return false
	}
}
