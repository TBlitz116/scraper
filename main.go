// Package main implements a faculty scraper for UMBC's CSEE department.
// It scrapes faculty names and titles from department pages, looks up
// their email addresses via the UMBC directory, and stores the results
// in a PostgreSQL database for professor verification during authentication.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// FacultyMember represents a scraped faculty entry.
type FacultyMember struct {
	Name  string
	Title string
	Email string
}

// CSEE faculty pages (structured as <a> links).
var cseeFacultyPages = []string{
	"https://www.csee.umbc.edu/people/tenure-track-faculty/",
	"https://www.csee.umbc.edu/people/instructional-faculty/",
}

// SE faculty page (structured as <p><strong>Name | Title</strong></p>).
const seFacultyPage = "https://professionalprograms.umbc.edu/software-engineering/software-engineering-faculty/"

const directoryURL = "https://www2.umbc.edu/search/directory/"
const department = "CSEE / Software Engineering"

func fetchHTMLPage(urlStr string) (*goquery.Document, error) {
	req, err := newScraperGET(urlStr)
	if err != nil {
		return nil, err
	}
	resp, err := scraperHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", urlStr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, urlStr)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", urlStr, err)
	}
	return doc, nil
}

// scrapeFacultyNames fetches faculty names and titles from CSEE and SE department pages.
func scrapeFacultyNames() ([]FacultyMember, error) {
	var faculty []FacultyMember
	seen := make(map[string]bool)

	// --- Scrape CSEE pages ---
	for _, url := range cseeFacultyPages {
		fmt.Printf("Scraping %s ...\n", url)

		doc, err := fetchHTMLPage(url)
		if err != nil {
			return nil, err
		}

		// Faculty name links point to individual profile pages (not category pages)
		// Filter: href must contain a faculty-specific path segment (not just /people/)
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists {
				return
			}

			// Must be a link to an individual faculty profile
			isFacultyProfile := false
			if strings.Contains(href, "/people/tenure-track-faculty/") && href != "https://www.csee.umbc.edu/people/tenure-track-faculty/" {
				isFacultyProfile = true
			}
			if strings.Contains(href, "/people/instructional-faculty/") && href != "https://www.csee.umbc.edu/people/instructional-faculty/" {
				isFacultyProfile = true
			}
			if strings.Contains(href, "/faculty/") && !strings.Contains(href, "/people/faculty-awards") {
				isFacultyProfile = true
			}
			if !isFacultyProfile {
				return
			}

			name := strings.TrimSpace(s.Text())
			if name == "" || len(name) > 50 {
				return
			}

			// Deduplicate
			if seen[name] {
				return
			}
			seen[name] = true

			// Title is in the next sibling <p>
			title := ""
			next := s.Next()
			if next.Is("p") {
				title = strings.TrimSpace(next.Text())
			}

			faculty = append(faculty, FacultyMember{
				Name:  name,
				Title: title,
			})
		})
	}

	// --- Scrape SE faculty page ---
	fmt.Printf("Scraping %s ...\n", seFacultyPage)
	seDoc, seErr := fetchHTMLPage(seFacultyPage)
	if seErr != nil {
		fmt.Printf("  Warning: failed to fetch SE page: %v\n", seErr)
	} else {
		// SE faculty: <p><strong>Name, Degree | Title</strong></p>
		seDoc.Find("p > strong").Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if text == "" || !strings.Contains(text, "|") {
				return
			}

			parts := strings.SplitN(text, "|", 2)
			nameRaw := strings.TrimSpace(parts[0])
			title := strings.TrimSpace(parts[1])

			// Remove degree suffixes like "Ph.D.", "J.D.", "M.B.A." from name
			nameParts := strings.Split(nameRaw, ",")
			name := strings.TrimSpace(nameParts[0])

			if name == "" || len(name) > 50 {
				return
			}

			if seen[name] {
				return
			}
			seen[name] = true

			faculty = append(faculty, FacultyMember{
				Name:  name,
				Title: title,
			})
		})
	}

	fmt.Printf("Found %d unique faculty members.\n", len(faculty))
	return faculty, nil
}

// lookupEmail queries the UMBC directory for a faculty member's email.
func lookupEmail(name string) string {
	req, err := newScraperGET(directoryURL)
	if err != nil {
		return ""
	}

	q := req.URL.Query()
	q.Set("search", name)
	req.URL.RawQuery = q.Encode()

	resp, err := scraperHTTPClient.Do(req)
	if err != nil {
		fmt.Printf("  Warning: directory lookup failed for %s: %v\n", name, err)
		return ""
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return ""
	}

	// Find mailto link
	email := ""
	doc.Find("a[href^='mailto:']").First().Each(func(i int, s *goquery.Selection) {
		email = strings.TrimSpace(s.Text())
	})

	return email
}

// scrapeAll runs the full pipeline: scrape names, then look up emails.
func scrapeAll() ([]FacultyMember, error) {
	faculty, err := scrapeFacultyNames()
	if err != nil {
		return nil, err
	}

	fmt.Println("\nLooking up emails from UMBC directory...")
	found := 0
	for i := range faculty {
		email := lookupEmail(faculty[i].Name)
		faculty[i].Email = email

		status := email
		if status == "" {
			status = "NOT FOUND"
		} else {
			found++
		}
		fmt.Printf("  [%d/%d] %s -> %s\n", i+1, len(faculty), faculty[i].Name, status)

		// Be polite to UMBC's server: ~500ms average with jitter
		time.Sleep(politePause(350, 750))
	}

	fmt.Printf("\nDone. %d/%d emails found.\n", found, len(faculty))
	return faculty, nil
}

// storeFaculty connects to PostgreSQL and stores the scraped faculty data.
func storeFaculty(faculty []FacultyMember) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	// Convert async URL format if needed
	dbURL = strings.Replace(dbURL, "postgresql+asyncpg://", "postgresql://", 1)
	// For local Docker: replace internal hostname with localhost
	dbURL = strings.Replace(dbURL, "@db:", "@localhost:", 1)
	// For local Docker: use exposed port 5433
	dbURL = strings.Replace(dbURL, ":5432/", ":5433/", 1)
	// Disable SSL for local Docker
	if !strings.Contains(dbURL, "sslmode=") {
		if strings.Contains(dbURL, "?") {
			dbURL += "&sslmode=disable"
		} else {
			dbURL += "?sslmode=disable"
		}
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	fmt.Println("Connected to database.")

	// Create table if not exists
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS verified_faculty (
			id SERIAL PRIMARY KEY,
			name VARCHAR NOT NULL,
			email VARCHAR UNIQUE,
			title VARCHAR,
			department VARCHAR DEFAULT 'Computer Science and Electrical Engineering',
			scraped_at TIMESTAMP DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Clear old data
	_, err = db.Exec("DELETE FROM verified_faculty")
	if err != nil {
		return fmt.Errorf("failed to clear table: %w", err)
	}

	// Insert faculty with emails
	inserted := 0
	for _, f := range faculty {
		if f.Email == "" {
			continue // Skip faculty without emails
		}

		_, err := db.Exec(`
			INSERT INTO verified_faculty (name, email, title, department, scraped_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (email) DO UPDATE SET
				name = EXCLUDED.name,
				title = EXCLUDED.title,
				scraped_at = EXCLUDED.scraped_at
		`, f.Name, f.Email, f.Title, department, time.Now().UTC())

		if err != nil {
			fmt.Printf("  Warning: failed to insert %s: %v\n", f.Name, err)
			continue
		}
		inserted++
	}

	fmt.Printf("\nStored %d verified faculty members in the database.\n", inserted)
	return nil
}

func main() {
	fmt.Println("=== UMBC Faculty Scraper (Go) ===\n")

	// Load .env from backend
	envPath := filepath.Join("..", "scheduler-backend", ".env")
	if err := godotenv.Load(envPath); err != nil {
		fmt.Printf("Warning: could not load %s: %v\n", envPath, err)
		fmt.Println("Falling back to environment variables.")
	}

	faculty, err := scrapeAll()
	if err != nil {
		log.Fatalf("Scraping failed: %v", err)
	}

	fmt.Println("\nConnecting to database...")
	if err := storeFaculty(faculty); err != nil {
		log.Fatalf("Storage failed: %v", err)
	}

	// Print summary
	fmt.Println("\n--- Results ---")
	for _, f := range faculty {
		email := f.Email
		if email == "" {
			email = "N/A"
		}
		fmt.Printf("%-30s | %-35s | %s\n", f.Name, f.Title, email)
	}

	fmt.Println("\nDone!")
}
