package contacts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/unstablemind/pocket/pkg/output"
)

// appleScriptTimeout is the maximum duration for any AppleScript execution.
const appleScriptTimeout = 30 * time.Second

// Contact represents a contact in Apple Contacts
type Contact struct {
	Name      string    `json:"name"`
	FirstName string    `json:"first_name,omitempty"`
	LastName  string    `json:"last_name,omitempty"`
	Company   string    `json:"company,omitempty"`
	Emails    []Email   `json:"emails,omitempty"`
	Phones    []Phone   `json:"phones,omitempty"`
	Addresses []Address `json:"addresses,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Birthday  string    `json:"birthday,omitempty"`
	JobTitle  string    `json:"job_title,omitempty"`
}

// Email represents an email address with label
type Email struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
}

// Phone represents a phone number with label
type Phone struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
}

// Address represents a physical address
type Address struct {
	Label   string `json:"label,omitempty"`
	Street  string `json:"street,omitempty"`
	City    string `json:"city,omitempty"`
	State   string `json:"state,omitempty"`
	Zip     string `json:"zip,omitempty"`
	Country string `json:"country,omitempty"`
}

// Group represents a contact group
type Group struct {
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

// ContactSummary represents a simplified contact for listing
type ContactSummary struct {
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Company string `json:"company,omitempty"`
}

// NewCmd creates the contacts command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "contacts",
		Aliases: []string{"contact", "addr", "addressbook"},
		Short:   "Apple Contacts commands (macOS only)",
		Long:    `Interact with Apple Contacts via AppleScript. Only available on macOS.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "darwin" {
				return output.PrintError("platform_unsupported",
					"Apple Contacts is only available on macOS",
					map[string]string{
						"current_platform": runtime.GOOS,
						"required":         "darwin (macOS)",
					})
			}
			return nil
		},
	}

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newSearchCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newGroupsCmd())
	cmd.AddCommand(newGroupCmd())
	cmd.AddCommand(newCreateCmd())

	return cmd
}

// escapeAppleScript escapes special characters for AppleScript strings
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// runOsascript executes an osascript command with a timeout and returns the output.
// The lang parameter specifies the scripting language ("AppleScript" or "JavaScript").
func runOsascript(lang string, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), appleScriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "osascript", "-l", lang, "-e", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("osascript timed out after %s", appleScriptTimeout)
		}
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(errMsg))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// runAppleScript executes an AppleScript with a timeout and returns the output
func runAppleScript(script string) (string, error) {
	return runOsascript("AppleScript", script)
}

// runJXA executes a JavaScript for Automation (JXA) script with a timeout
func runJXA(script string) (string, error) {
	return runOsascript("JavaScript", script)
}

// newListCmd lists all contacts
func newListCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use JXA for fast batch property access instead of AppleScript's
			// per-contact iteration which is extremely slow for large databases.
			maxResults := limit
			if maxResults <= 0 {
				maxResults = 100
			}

			script := fmt.Sprintf(`
var app = Application('Contacts');
var maxResults = %d;

// Batch-fetch all properties in 4 Apple Event calls
var names = app.people.name();
var orgs = app.people.organization();
var allEmails = app.people.emails.value();
var allPhones = app.people.phones.value();
var total = names.length;
var count = Math.min(total, maxResults);

// Build results entirely from batch-fetched data (no per-contact calls)
var results = [];
for (var i = 0; i < count; i++) {
    var name = names[i] || '';
    var company = (orgs[i] && typeof orgs[i] === 'string') ? orgs[i] : '';
    var email = (allEmails[i] && allEmails[i].length > 0) ? allEmails[i][0] : '';
    var phone = (allPhones[i] && allPhones[i].length > 0) ? allPhones[i][0] : '';
    results.push(name + '|||' + email + '|||' + phone + '|||' + company);
}
total + '~~~' + results.join(':::');
`, maxResults)

			result, err := runJXA(script)
			if err != nil {
				return output.PrintError("list_failed", err.Error(), nil)
			}

			if result == "" {
				return output.Print(map[string]any{
					"contacts": []ContactSummary{},
					"count":    0,
					"total":    0,
				})
			}

			// Parse total count and results
			totalParts := strings.SplitN(result, "~~~", 2)
			total := 0
			_, _ = fmt.Sscanf(totalParts[0], "%d", &total)

			contactData := ""
			if len(totalParts) > 1 {
				contactData = totalParts[1]
			}

			if contactData == "" {
				return output.Print(map[string]any{
					"contacts": []ContactSummary{},
					"count":    0,
					"total":    total,
				})
			}

			// Parse the result
			var contacts []ContactSummary
			items := strings.Split(contactData, ":::")
			for _, item := range items {
				parts := strings.Split(item, "|||")
				if len(parts) >= 4 {
					c := ContactSummary{
						Name:    strings.TrimSpace(parts[0]),
						Email:   strings.TrimSpace(parts[1]),
						Phone:   strings.TrimSpace(parts[2]),
						Company: strings.TrimSpace(parts[3]),
					}
					if c.Company == "null" {
						c.Company = ""
					}
					contacts = append(contacts, c)
				}
			}

			return output.Print(map[string]any{
				"contacts": contacts,
				"count":    len(contacts),
				"total":    total,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "Limit number of contacts (0 = all, default 100)")

	return cmd
}

// escapeJSString escapes special characters for JavaScript string literals
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// newSearchCmd searches contacts
//
//nolint:gocyclo // sequential JXA script construction with clear logic
func newSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search contacts by name, email, or phone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Default limit for search to avoid unbounded results
			maxResults := limit
			if maxResults <= 0 {
				maxResults = 50
			}

			// Use JXA (JavaScript for Automation) for fast batch property access.
			// AppleScript's "repeat with p in people" makes individual Apple Event
			// IPC calls per contact, which is extremely slow for large databases.
			// JXA's batch property access (e.g., app.people.name()) fetches all
			// values in a single Apple Event call, making it orders of magnitude
			// faster.
			script := fmt.Sprintf(`
var app = Application('Contacts');
var query = '%s'.toLowerCase();
var maxResults = %d;

// Batch-fetch all properties in just 4 Apple Event calls (instead of N*4)
var names = app.people.name();
var orgs = app.people.organization();
var allEmails = app.people.emails.value();
var allPhones = app.people.phones.value();

// Find matching contact indices (all in-memory, fast)
var matchIndices = [];
var matched = {};
for (var i = 0; i < names.length && matchIndices.length < maxResults; i++) {
    var n = (names[i] || '').toLowerCase();
    var o = (orgs[i] && typeof orgs[i] === 'string') ? orgs[i].toLowerCase() : '';

    if (n.indexOf(query) >= 0 || o.indexOf(query) >= 0) {
        matchIndices.push(i);
        matched[i] = true;
        continue;
    }

    // Check emails
    var found = false;
    var emails = allEmails[i] || [];
    for (var e = 0; e < emails.length; e++) {
        if (emails[e] && emails[e].toLowerCase().indexOf(query) >= 0) {
            found = true; break;
        }
    }

    // Check phones
    if (!found) {
        var phones = allPhones[i] || [];
        for (var ph = 0; ph < phones.length; ph++) {
            if (phones[ph] && phones[ph].indexOf(query) >= 0) {
                found = true; break;
            }
        }
    }

    if (found) {
        matchIndices.push(i);
        matched[i] = true;
    }
}

// Fetch primary email/phone only for matched contacts
var results = [];
for (var j = 0; j < matchIndices.length; j++) {
    var idx = matchIndices[j];
    var name = names[idx] || '';
    var company = (orgs[idx] && typeof orgs[idx] === 'string') ? orgs[idx] : '';
    var email = (allEmails[idx] && allEmails[idx].length > 0) ? allEmails[idx][0] : '';
    var phone = (allPhones[idx] && allPhones[idx].length > 0) ? allPhones[idx][0] : '';
    results.push(name + '|||' + email + '|||' + phone + '|||' + company);
}
results.join(':::');
`, escapeJSString(query), maxResults)

			result, err := runJXA(script)
			if err != nil {
				return output.PrintError("search_failed", err.Error(), nil)
			}

			if result == "" {
				return output.Print(map[string]any{
					"query":    query,
					"contacts": []ContactSummary{},
					"count":    0,
				})
			}

			// Parse the result
			var contacts []ContactSummary
			items := strings.Split(result, ":::")
			for _, item := range items {
				parts := strings.Split(item, "|||")
				if len(parts) >= 4 {
					c := ContactSummary{
						Name:    strings.TrimSpace(parts[0]),
						Email:   strings.TrimSpace(parts[1]),
						Phone:   strings.TrimSpace(parts[2]),
						Company: strings.TrimSpace(parts[3]),
					}
					// JXA returns "null" for missing properties
					if c.Company == "null" {
						c.Company = ""
					}
					contacts = append(contacts, c)
				}
			}

			return output.Print(map[string]any{
				"query":    query,
				"contacts": contacts,
				"count":    len(contacts),
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "Limit number of results (0 = all, default 50)")

	return cmd
}

// newGetCmd gets full contact details by name
//
//nolint:gocyclo // complex but clear sequential logic
func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [name]",
		Short: "Get full contact details by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contactName := args[0]

			script := fmt.Sprintf(`
tell application "Contacts"
	try
		set p to first person whose name is "%s"

		-- Basic info
		set fullName to name of p
		set firstName to ""
		set lastName to ""
		set companyName to ""
		set jobTitle to ""
		set notesText to ""
		set birthdayText to ""

		try
			set firstName to first name of p
		end try
		try
			set lastName to last name of p
		end try
		try
			set companyName to organization of p
		end try
		try
			set jobTitle to job title of p
		end try
		try
			set notesText to note of p
		end try
		try
			set birthdayText to birth date of p as string
		end try

		-- Emails
		set emailList to ""
		repeat with e in emails of p
			set emailLabel to label of e
			set emailValue to value of e
			set emailList to emailList & emailLabel & "=" & emailValue & ";;;"
		end repeat

		-- Phones
		set phoneList to ""
		repeat with ph in phones of p
			set phoneLabel to label of ph
			set phoneValue to value of ph
			set phoneList to phoneList & phoneLabel & "=" & phoneValue & ";;;"
		end repeat

		-- Addresses
		set addressList to ""
		repeat with addr in addresses of p
			set addrLabel to label of addr
			set addrStreet to ""
			set addrCity to ""
			set addrState to ""
			set addrZip to ""
			set addrCountry to ""
			try
				set addrStreet to street of addr
			end try
			try
				set addrCity to city of addr
			end try
			try
				set addrState to state of addr
			end try
			try
				set addrZip to zip of addr
			end try
			try
				set addrCountry to country of addr
			end try
			set addressList to addressList & addrLabel & "=" & addrStreet & "|" & addrCity & "|" & addrState & "|" & addrZip & "|" & addrCountry & ";;;"
		end repeat

		return fullName & "|||" & firstName & "|||" & lastName & "|||" & companyName & "|||" & jobTitle & "|||" & notesText & "|||" & birthdayText & "|||" & emailList & "|||" & phoneList & "|||" & addressList
	on error errMsg
		return "ERROR: " & errMsg
	end try
end tell`, escapeAppleScript(contactName))

			result, err := runAppleScript(script)
			if err != nil {
				return output.PrintError("get_failed", err.Error(), nil)
			}

			if strings.HasPrefix(result, "ERROR:") {
				errMsg := strings.TrimPrefix(result, "ERROR: ")
				if strings.Contains(errMsg, "Can't get person") {
					return output.PrintError("contact_not_found",
						fmt.Sprintf("Contact not found: %s", contactName),
						map[string]string{"name": contactName})
				}
				return output.PrintError("get_failed", errMsg, nil)
			}

			// Parse the result
			parts := strings.Split(result, "|||")
			if len(parts) < 10 {
				return output.PrintError("parse_failed", "Failed to parse contact data", nil)
			}

			contact := Contact{
				Name:      strings.TrimSpace(parts[0]),
				FirstName: strings.TrimSpace(parts[1]),
				LastName:  strings.TrimSpace(parts[2]),
				Company:   strings.TrimSpace(parts[3]),
				JobTitle:  strings.TrimSpace(parts[4]),
				Notes:     strings.TrimSpace(parts[5]),
				Birthday:  strings.TrimSpace(parts[6]),
			}

			// Parse emails
			emailStr := strings.TrimSpace(parts[7])
			if emailStr != "" {
				emailItems := strings.Split(emailStr, ";;;")
				for _, item := range emailItems {
					item = strings.TrimSpace(item)
					if item == "" {
						continue
					}
					emailParts := strings.SplitN(item, "=", 2)
					if len(emailParts) == 2 {
						contact.Emails = append(contact.Emails, Email{
							Label: cleanLabel(emailParts[0]),
							Value: emailParts[1],
						})
					}
				}
			}

			// Parse phones
			phoneStr := strings.TrimSpace(parts[8])
			if phoneStr != "" {
				phoneItems := strings.Split(phoneStr, ";;;")
				for _, item := range phoneItems {
					item = strings.TrimSpace(item)
					if item == "" {
						continue
					}
					phoneParts := strings.SplitN(item, "=", 2)
					if len(phoneParts) == 2 {
						contact.Phones = append(contact.Phones, Phone{
							Label: cleanLabel(phoneParts[0]),
							Value: phoneParts[1],
						})
					}
				}
			}

			// Parse addresses
			addrStr := strings.TrimSpace(parts[9])
			if addrStr != "" {
				addrItems := strings.Split(addrStr, ";;;")
				for _, item := range addrItems {
					item = strings.TrimSpace(item)
					if item == "" {
						continue
					}
					addrParts := strings.SplitN(item, "=", 2)
					if len(addrParts) == 2 {
						addrFields := strings.Split(addrParts[1], "|")
						if len(addrFields) >= 5 {
							contact.Addresses = append(contact.Addresses, Address{
								Label:   cleanLabel(addrParts[0]),
								Street:  addrFields[0],
								City:    addrFields[1],
								State:   addrFields[2],
								Zip:     addrFields[3],
								Country: addrFields[4],
							})
						}
					}
				}
			}

			return output.Print(contact)
		},
	}

	return cmd
}

// cleanLabel removes the special characters from AppleScript labels like "_$!<Home>!$_"
func cleanLabel(label string) string {
	label = strings.TrimPrefix(label, "_$!<")
	label = strings.TrimSuffix(label, ">!$_")
	return label
}

// newGroupsCmd lists all contact groups
func newGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "List all contact groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			script := `
tell application "Contacts"
	set groupList to {}
	repeat with g in groups
		set groupName to name of g
		set groupCount to count of people of g
		set end of groupList to groupName & "|||" & groupCount
	end repeat
	set AppleScript's text item delimiters to ":::"
	return groupList as text
end tell`

			result, err := runAppleScript(script)
			if err != nil {
				return output.PrintError("groups_failed", err.Error(), nil)
			}

			if result == "" {
				return output.Print(map[string]any{
					"groups": []Group{},
					"count":  0,
				})
			}

			// Parse the result
			var groups []Group
			items := strings.Split(result, ":::")
			for _, item := range items {
				parts := strings.Split(item, "|||")
				if len(parts) >= 2 {
					count := 0
					_, _ = fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &count)
					groups = append(groups, Group{
						Name:  strings.TrimSpace(parts[0]),
						Count: count,
					})
				}
			}

			return output.Print(map[string]any{
				"groups": groups,
				"count":  len(groups),
			})
		},
	}

	return cmd
}

// newGroupCmd lists contacts in a specific group
func newGroupCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "group [name]",
		Short: "List contacts in a specific group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupName := args[0]

			script := fmt.Sprintf(`
tell application "Contacts"
	try
		set g to group "%s"
		set contactList to {}
		repeat with p in people of g
			set fullName to name of p
			set primaryEmail to ""
			set primaryPhone to ""
			set companyName to ""
			try
				set primaryEmail to value of first email of p
			end try
			try
				set primaryPhone to value of first phone of p
			end try
			try
				set companyName to organization of p
			end try
			set end of contactList to fullName & "|||" & primaryEmail & "|||" & primaryPhone & "|||" & companyName
		end repeat
		set AppleScript's text item delimiters to ":::"
		return contactList as text
	on error errMsg
		return "ERROR: " & errMsg
	end try
end tell`, escapeAppleScript(groupName))

			result, err := runAppleScript(script)
			if err != nil {
				return output.PrintError("group_failed", err.Error(), nil)
			}

			if strings.HasPrefix(result, "ERROR:") {
				errMsg := strings.TrimPrefix(result, "ERROR: ")
				if strings.Contains(errMsg, "Can't get group") {
					return output.PrintError("group_not_found",
						fmt.Sprintf("Group not found: %s", groupName),
						map[string]string{"name": groupName})
				}
				return output.PrintError("group_failed", errMsg, nil)
			}

			if result == "" {
				return output.Print(map[string]any{
					"group":    groupName,
					"contacts": []ContactSummary{},
					"count":    0,
				})
			}

			// Parse the result
			var contacts []ContactSummary
			items := strings.Split(result, ":::")
			count := 0
			for _, item := range items {
				if limit > 0 && count >= limit {
					break
				}
				parts := strings.Split(item, "|||")
				if len(parts) >= 4 {
					contacts = append(contacts, ContactSummary{
						Name:    strings.TrimSpace(parts[0]),
						Email:   strings.TrimSpace(parts[1]),
						Phone:   strings.TrimSpace(parts[2]),
						Company: strings.TrimSpace(parts[3]),
					})
					count++
				}
			}

			return output.Print(map[string]any{
				"group":    groupName,
				"contacts": contacts,
				"count":    len(contacts),
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "Limit number of contacts (0 = no limit)")

	return cmd
}

// newCreateCmd creates a new contact
func newCreateCmd() *cobra.Command {
	var email string
	var phone string
	var company string
	var note string

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new contact",
		Long:  `Create a new contact with the specified name. Optionally add email, phone, company, and notes.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Parse name into first and last
			nameParts := strings.SplitN(name, " ", 2)
			firstName := nameParts[0]
			lastName := ""
			if len(nameParts) > 1 {
				lastName = nameParts[1]
			}

			// Build properties string
			var propsBuilder strings.Builder
			propsBuilder.WriteString(fmt.Sprintf(`{first name:"%s"`, escapeAppleScript(firstName))) //nolint:gocritic // AppleScript property syntax requires this format
			if lastName != "" {
				propsBuilder.WriteString(fmt.Sprintf(`, last name:"%s"`, escapeAppleScript(lastName))) //nolint:gocritic // AppleScript property syntax requires this format
			}
			if company != "" {
				propsBuilder.WriteString(fmt.Sprintf(`, organization:"%s"`, escapeAppleScript(company))) //nolint:gocritic // AppleScript property syntax requires this format
			}
			if note != "" {
				propsBuilder.WriteString(fmt.Sprintf(`, note:"%s"`, escapeAppleScript(note))) //nolint:gocritic // AppleScript property syntax requires this format
			}
			propsBuilder.WriteString("}")

			// Build the script
			var scriptBuilder strings.Builder
			scriptBuilder.WriteString(fmt.Sprintf(`
tell application "Contacts"
	try
		set newPerson to make new person with properties %s
`, propsBuilder.String()))

			// Add email if provided
			if email != "" {
				scriptBuilder.WriteString(fmt.Sprintf(`		make new email at end of emails of newPerson with properties {label:"work", value:"%s"}
`, escapeAppleScript(email)))
			}

			// Add phone if provided
			if phone != "" {
				scriptBuilder.WriteString(fmt.Sprintf(`		make new phone at end of phones of newPerson with properties {label:"mobile", value:"%s"}
`, escapeAppleScript(phone)))
			}

			scriptBuilder.WriteString(`		save
		return name of newPerson
	on error errMsg
		return "ERROR: " & errMsg
	end try
end tell
`)

			result, err := runAppleScript(scriptBuilder.String())
			if err != nil {
				return output.PrintError("create_failed", err.Error(), nil)
			}

			if strings.HasPrefix(result, "ERROR:") {
				return output.PrintError("create_failed", strings.TrimPrefix(result, "ERROR: "), nil)
			}

			response := map[string]any{
				"success": true,
				"message": "Contact created successfully",
				"name":    result,
			}
			if email != "" {
				response["email"] = email
			}
			if phone != "" {
				response["phone"] = phone
			}
			if company != "" {
				response["company"] = company
			}
			if note != "" {
				response["note"] = note
			}

			return output.Print(response)
		},
	}

	cmd.Flags().StringVarP(&email, "email", "e", "", "Email address")
	cmd.Flags().StringVarP(&phone, "phone", "p", "", "Phone number")
	cmd.Flags().StringVarP(&company, "company", "c", "", "Company/organization name")
	cmd.Flags().StringVarP(&note, "note", "n", "", "Notes about the contact")

	return cmd
}
