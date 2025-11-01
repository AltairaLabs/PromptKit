package middleware

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

var (
	domainKeywords = map[string]string{
		"app":          "mobile app development",
		"mobile":       "mobile app development",
		"ios":          "mobile app development",
		"android":      "mobile app development",
		"website":      "web development",
		"web":          "web development",
		"react":        "web development",
		"frontend":     "web development",
		"backend":      "backend development",
		"api":          "backend development",
		"server":       "backend development",
		"database":     "backend development",
		"startup":      "business development",
		"business":     "business development",
		"marketing":    "marketing",
		"design":       "product design",
		"coding":       "software development",
		"programming":  "software development",
		"developer":    "software development",
		"ai":           "artificial intelligence",
		"machine":      "machine learning",
		"ml":           "machine learning",
		"data":         "data science",
		"analytics":    "data science",
		"finance":      "finance",
		"fintech":      "financial technology",
		"money":        "finance",
		"health":       "healthcare",
		"fitness":      "healthcare",
		"education":    "education",
		"learning":     "education",
		"game":         "game development",
		"gaming":       "game development",
		"content":      "content creation",
		"writing":      "content creation",
		"productivity": "productivity",
		"organize":     "productivity",
		"ecommerce":    "e-commerce",
	}

	roleKeywords = map[string]string{
		"entrepreneur": "entrepreneur",
		"founder":      "entrepreneur",
		"startup":      "entrepreneur",
		"developer":    "software developer",
		"programmer":   "software developer",
		"coder":        "software developer",
		"engineer":     "software engineer",
		"designer":     "product designer",
		"ux":           "ux designer",
		"ui":           "ui designer",
		"manager":      "product manager",
		"pm":           "product manager",
		"ceo":          "executive",
		"cto":          "technical executive",
		"freelancer":   "freelancer",
		"consultant":   "consultant",
		"student":      "student",
		"beginner":     "beginner",
		"newbie":       "beginner",
		"learning":     "learner",
		"business":     "business owner",
		"my company":   "business owner",
		"my team":      "team lead",
		"my project":   "project owner",
	}
)

// extractDomainFromText finds domain keywords in text
func extractDomainFromText(text string) string {
	text = strings.ToLower(text)
	for keyword, domain := range domainKeywords {
		if strings.Contains(text, keyword) {
			return domain
		}
	}
	return "general"
}

// extractRoleFromText finds role keywords in text
func extractRoleFromText(text string) string {
	text = strings.ToLower(text)
	for keyword, role := range roleKeywords {
		if strings.Contains(text, keyword) {
			return role
		}
	}
	return "user"
}

// summarizeMessages creates a contextual summary from messages
func summarizeMessages(messages []types.Message) string {
	if len(messages) == 0 {
		return "New conversation - no context available"
	}

	var conversationLines []string
	startIdx := len(messages) - 4
	if startIdx < 0 {
		startIdx = 0
	}

	for i := startIdx; i < len(messages); i++ {
		msg := messages[i]
		role := "User"
		if msg.Role == "assistant" {
			role = "Assistant"
		}
		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		conversationLines = append(conversationLines, fmt.Sprintf("%s: %s", role, content))
	}

	if len(conversationLines) > 0 {
		return strings.Join(conversationLines, "\n")
	}

	return "Conversation in progress"
}
