// Package interview provides interview orchestration and state management.
package interview

import (
	"fmt"
	"strings"
)

// Question represents a single interview question
type Question struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Answer   string `json:"answer"`
	Hint     string `json:"hint"`
	Category string `json:"category"`
}

// QuestionBank holds a set of questions for a topic
type QuestionBank struct {
	Topic       string     `json:"topic"`
	Description string     `json:"description"`
	Questions   []Question `json:"questions"`
}

// BuiltInTopics returns the available built-in interview topics
func BuiltInTopics() []string {
	return []string{
		"classic-rock",
		"space-exploration",
		"programming",
		"world-history",
		"movies",
	}
}

// GetQuestionBank returns the question bank for a given topic
func GetQuestionBank(topic string) (*QuestionBank, error) {
	switch strings.ToLower(topic) {
	case "classic-rock":
		return classicRockQuestions(), nil
	case "space-exploration", "space":
		return spaceExplorationQuestions(), nil
	case "programming", "coding":
		return programmingQuestions(), nil
	case "world-history", "history":
		return worldHistoryQuestions(), nil
	case "movies", "film":
		return movieQuestions(), nil
	default:
		return nil, fmt.Errorf("unknown topic: %s. Available: %v", topic, BuiltInTopics())
	}
}

// FormatQuestionsForPrompt formats questions for the system prompt
func (qb *QuestionBank) FormatQuestionsForPrompt() string {
	var sb strings.Builder
	for i, q := range qb.Questions {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q.Text))
		sb.WriteString(fmt.Sprintf("   Expected Answer: %s\n", q.Answer))
		if q.Hint != "" {
			sb.WriteString(fmt.Sprintf("   Hint (if needed): %s\n", q.Hint))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func classicRockQuestions() *QuestionBank {
	return &QuestionBank{
		Topic:       "Classic Rock Music",
		Description: "Test your knowledge of legendary rock bands and albums",
		Questions: []Question{
			{
				ID:       "rock-1",
				Text:     "Which band released the album 'Dark Side of the Moon' in 1973?",
				Answer:   "Pink Floyd",
				Hint:     "They're known for psychedelic and progressive rock",
				Category: "albums",
			},
			{
				ID:       "rock-2",
				Text:     "What was Led Zeppelin's original band name before they became Led Zeppelin?",
				Answer:   "The New Yardbirds",
				Hint:     "It was related to the band The Yardbirds",
				Category: "history",
			},
			{
				ID:       "rock-3",
				Text:     "In what year was the first Woodstock music festival held?",
				Answer:   "1969",
				Hint:     "It was in the late 1960s, during the Summer of Love era",
				Category: "events",
			},
			{
				ID:       "rock-4",
				Text:     "What was the Beatles' final studio album?",
				Answer:   "Let It Be (or Abbey Road)",
				Hint:     "Abbey Road was recorded last but Let It Be was released last",
				Category: "albums",
			},
			{
				ID:       "rock-5",
				Text:     "Which guitarist is known for playing a guitar on fire at the Monterey Pop Festival?",
				Answer:   "Jimi Hendrix",
				Hint:     "He was known for his innovative electric guitar techniques",
				Category: "artists",
			},
		},
	}
}

func spaceExplorationQuestions() *QuestionBank {
	return &QuestionBank{
		Topic:       "Space Exploration",
		Description: "Test your knowledge of humanity's journey to the stars",
		Questions: []Question{
			{
				ID:       "space-1",
				Text:     "Who was the first human to walk on the Moon?",
				Answer:   "Neil Armstrong",
				Hint:     "He said 'That's one small step for man...'",
				Category: "astronauts",
			},
			{
				ID:       "space-2",
				Text:     "What was the name of the first artificial satellite launched into space?",
				Answer:   "Sputnik 1",
				Hint:     "It was launched by the Soviet Union in 1957",
				Category: "spacecraft",
			},
			{
				ID:       "space-3",
				Text:     "Which planet in our solar system has the most moons?",
				Answer:   "Saturn",
				Hint:     "It's known for its distinctive rings",
				Category: "planets",
			},
			{
				ID:       "space-4",
				Text:     "What is the name of NASA's most powerful space telescope launched in 2021?",
				Answer:   "James Webb Space Telescope",
				Hint:     "It's named after a former NASA administrator",
				Category: "technology",
			},
			{
				ID:       "space-5",
				Text:     "Which company successfully landed a rocket booster for reuse for the first time?",
				Answer:   "SpaceX",
				Hint:     "It was founded by Elon Musk",
				Category: "companies",
			},
		},
	}
}

func programmingQuestions() *QuestionBank {
	return &QuestionBank{
		Topic:       "Programming & Computer Science",
		Description: "Test your knowledge of coding concepts and tech history",
		Questions: []Question{
			{
				ID:       "prog-1",
				Text:     "What programming language was created by Guido van Rossum in 1991?",
				Answer:   "Python",
				Hint:     "It's named after a British comedy group",
				Category: "languages",
			},
			{
				ID:       "prog-2",
				Text:     "What does HTTP stand for?",
				Answer:   "Hypertext Transfer Protocol",
				Hint:     "It's the protocol used for web communication",
				Category: "web",
			},
			{
				ID:       "prog-3",
				Text:     "Who is considered the father of computer science?",
				Answer:   "Alan Turing",
				Hint:     "He created a theoretical computing machine named after him",
				Category: "history",
			},
			{
				ID:       "prog-4",
				Text:     "What does API stand for?",
				Answer:   "Application Programming Interface",
				Hint:     "It's how different software components communicate",
				Category: "concepts",
			},
			{
				ID:       "prog-5",
				Text:     "Which company developed the Go programming language?",
				Answer:   "Google",
				Hint:     "They also developed Android and Chrome",
				Category: "languages",
			},
		},
	}
}

func worldHistoryQuestions() *QuestionBank {
	return &QuestionBank{
		Topic:       "World History",
		Description: "Test your knowledge of major historical events and figures",
		Questions: []Question{
			{
				ID:       "hist-1",
				Text:     "In what year did World War II end?",
				Answer:   "1945",
				Hint:     "It was in the mid-1940s",
				Category: "wars",
			},
			{
				ID:       "hist-2",
				Text:     "Who was the first President of the United States?",
				Answer:   "George Washington",
				Hint:     "The capital city is named after him",
				Category: "leaders",
			},
			{
				ID:       "hist-3",
				Text:     "The Great Wall was built primarily to protect against invasions from which direction?",
				Answer:   "North (or Northern nomads/Mongols)",
				Hint:     "Think about where Mongolia is relative to China",
				Category: "landmarks",
			},
			{
				ID:       "hist-4",
				Text:     "Who painted the ceiling of the Sistine Chapel?",
				Answer:   "Michelangelo",
				Hint:     "He was also famous as a sculptor who created David",
				Category: "art",
			},
			{
				ID:       "hist-5",
				Text:     "The French Revolution began in which year?",
				Answer:   "1789",
				Hint:     "It was the year the Bastille was stormed",
				Category: "revolutions",
			},
		},
	}
}

func movieQuestions() *QuestionBank {
	return &QuestionBank{
		Topic:       "Movies & Cinema",
		Description: "Test your knowledge of classic and modern films",
		Questions: []Question{
			{
				ID:       "movie-1",
				Text:     "What was the first feature-length animated film by Disney?",
				Answer:   "Snow White and the Seven Dwarfs",
				Hint:     "It was released in 1937 and features a princess",
				Category: "animation",
			},
			{
				ID:       "movie-2",
				Text:     "Who directed the movie 'Jurassic Park'?",
				Answer:   "Steven Spielberg",
				Hint:     "He also directed E.T. and Schindler's List",
				Category: "directors",
			},
			{
				ID:       "movie-3",
				Text:     "What is the highest-grossing film of all time (not adjusted for inflation)?",
				Answer:   "Avatar",
				Hint:     "It's set on a moon called Pandora",
				Category: "box-office",
			},
			{
				ID:       "movie-4",
				Text:     "In 'The Matrix', what color pill does Neo take?",
				Answer:   "Red",
				Hint:     "It shows him the truth about reality",
				Category: "sci-fi",
			},
			{
				ID:       "movie-5",
				Text:     "What is the name of the fictional African country in 'Black Panther'?",
				Answer:   "Wakanda",
				Hint:     "It's known for vibranium",
				Category: "marvel",
			},
		},
	}
}
