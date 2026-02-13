package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/unstablemind/pocket/pkg/output"
)

// Using MyMemory API (free, no auth needed)
// https://mymemory.translated.net/doc/spec.php

var baseURL = "https://api.mymemory.translated.net"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Translation is LLM-friendly translation output
type Translation struct {
	SourceText     string  `json:"source_text"`
	TranslatedText string  `json:"translated_text"`
	SourceLang     string  `json:"source_lang"`
	TargetLang     string  `json:"target_lang"`
	Match          float64 `json:"match,omitempty"`
}

// Language represents a supported language
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "translate",
		Aliases: []string{"trans", "tr"},
		Short:   "Translation commands (MyMemory)",
	}

	cmd.AddCommand(newTextCmd())
	cmd.AddCommand(newLanguagesCmd())

	return cmd
}

func newTextCmd() *cobra.Command {
	var fromLang, toLang string

	cmd := &cobra.Command{
		Use:   "text [text]",
		Short: "Translate text between languages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args, " ")

			// Build the langpair as "from|to". The pipe must NOT be percent-encoded
			// because the MyMemory API requires a literal pipe separator.
			// url.QueryEscape would encode | to %7C, breaking the API call.
			langpair := fmt.Sprintf("%s|%s", url.QueryEscape(fromLang), url.QueryEscape(toLang))
			reqURL := fmt.Sprintf("%s/get?q=%s&langpair=%s",
				baseURL,
				url.QueryEscape(text),
				langpair)

			resp, err := doRequest(reqURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			var data struct {
				ResponseStatus int `json:"responseStatus"`
				ResponseData   struct {
					TranslatedText string  `json:"translatedText"`
					Match          float64 `json:"match"`
				} `json:"responseData"`
				ResponseDetails string `json:"responseDetails"`
				Matches         []struct {
					Translation string  `json:"translation"`
					Match       float64 `json:"match"`
					Quality     any     `json:"quality"`
				} `json:"matches"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				return output.PrintError("parse_failed", err.Error(), nil)
			}

			if data.ResponseStatus != 200 {
				msg := "Translation failed"
				if data.ResponseDetails != "" {
					msg = data.ResponseDetails
				}
				return output.PrintError("api_error", msg, nil)
			}

			// Select the best translation from matches. The responseData.translatedText
			// can sometimes return garbage from low-quality community contributions.
			// We look through all matches and pick the most frequently occurring
			// translation among those with the highest match score.
			translatedText := data.ResponseData.TranslatedText
			matchScore := data.ResponseData.Match

			if len(data.Matches) > 1 {
				translatedText, matchScore = bestTranslation(data.Matches)
			}

			translation := Translation{
				SourceText:     text,
				TranslatedText: translatedText,
				SourceLang:     fromLang,
				TargetLang:     toLang,
				Match:          matchScore,
			}

			return output.Print(translation)
		},
	}

	cmd.Flags().StringVarP(&fromLang, "from", "f", "en", "Source language code (e.g., en, es, fr)")
	cmd.Flags().StringVarP(&toLang, "to", "t", "es", "Target language code (e.g., en, es, fr)")

	return cmd
}

func newLanguagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "languages",
		Short: "List common supported languages",
		RunE: func(cmd *cobra.Command, args []string) error {
			// MyMemory supports many languages, here are the most common ones
			languages := []Language{
				{Code: "en", Name: "English"},
				{Code: "es", Name: "Spanish"},
				{Code: "fr", Name: "French"},
				{Code: "de", Name: "German"},
				{Code: "it", Name: "Italian"},
				{Code: "pt", Name: "Portuguese"},
				{Code: "ru", Name: "Russian"},
				{Code: "zh", Name: "Chinese (Simplified)"},
				{Code: "ja", Name: "Japanese"},
				{Code: "ko", Name: "Korean"},
				{Code: "ar", Name: "Arabic"},
				{Code: "hi", Name: "Hindi"},
				{Code: "nl", Name: "Dutch"},
				{Code: "pl", Name: "Polish"},
				{Code: "tr", Name: "Turkish"},
				{Code: "vi", Name: "Vietnamese"},
				{Code: "th", Name: "Thai"},
				{Code: "id", Name: "Indonesian"},
				{Code: "ms", Name: "Malay"},
				{Code: "sv", Name: "Swedish"},
				{Code: "da", Name: "Danish"},
				{Code: "no", Name: "Norwegian"},
				{Code: "fi", Name: "Finnish"},
				{Code: "el", Name: "Greek"},
				{Code: "he", Name: "Hebrew"},
				{Code: "cs", Name: "Czech"},
				{Code: "ro", Name: "Romanian"},
				{Code: "hu", Name: "Hungarian"},
				{Code: "uk", Name: "Ukrainian"},
				{Code: "bn", Name: "Bengali"},
			}

			return output.Print(languages)
		},
	}

	return cmd
}

// bestTranslation picks the most reliable translation from the matches array.
// MyMemory's top responseData result can be wrong due to bad community data.
// This function finds the highest match score, then among all translations
// with that score, returns the one that appears most frequently.
func bestTranslation(matches []struct {
	Translation string  `json:"translation"`
	Match       float64 `json:"match"`
	Quality     any     `json:"quality"`
}) (string, float64) {
	if len(matches) == 0 {
		return "", 0
	}

	// Find the highest match score
	bestScore := matches[0].Match
	for _, m := range matches[1:] {
		if m.Match > bestScore {
			bestScore = m.Match
		}
	}

	// Count occurrences of each translation at the best score
	counts := make(map[string]int)
	for _, m := range matches {
		if m.Match == bestScore {
			counts[strings.ToLower(m.Translation)]++
		}
	}

	// Find the most frequent translation
	bestTrans := matches[0].Translation
	bestCount := 0
	for _, m := range matches {
		if m.Match == bestScore {
			c := counts[strings.ToLower(m.Translation)]
			if c > bestCount {
				bestCount = c
				bestTrans = m.Translation
			}
		}
	}

	return bestTrans, bestScore
}

func doRequest(reqURL string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, http.NoBody)
	if err != nil {
		return nil, output.PrintError("fetch_failed", err.Error(), nil)
	}

	req.Header.Set("User-Agent", "Pocket-CLI/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, output.PrintError("fetch_failed", err.Error(), nil)
	}

	if resp.StatusCode == 429 {
		resp.Body.Close()
		return nil, output.PrintError("rate_limited", "Rate limit exceeded, try again later", nil)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, output.PrintError("fetch_failed", fmt.Sprintf("HTTP %d", resp.StatusCode), nil)
	}

	return resp, nil
}
