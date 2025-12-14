package main

import (
	"context"
	"encoding/json" // for unmarshal problems
	"fmt"
	"log"
	"net/url"
	"os" // for save file operations
	"path/filepath"
	"strings" // String operations is able to record link hrefs
	"time"    // need to set timeout

	// for network conditions and http code
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func main() {
	// URL check
	if len(os.Args) < 2 {
		log.Fatal("Please provide a URL as a command-line argument.")
	}
	rawURL := os.Args[1]
	fmt.Printf("Navigating to URL: %s\n", rawURL)

	// Create files
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		log.Fatal("Invalid URL: ", err)
	}
	hostname := parsedURL.Hostname()

	// The time to be added for files name
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	folderPath := filepath.Join("scraped_data", fmt.Sprintf("%s_%s", timestamp, hostname))

	// 0755 -> rwxr-xr-x
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		log.Fatal("Failed to create directory: ", err)
	}

	fmt.Print("The Registry folder is created.", folderPath)

	/*
		// Create context
		// := -> Short variable declaration -> makes both variable and assigns value
		ctx, cancel := chromedp.NewContext(context.Background())
		defer cancel() // Browser will be closed when main function exits

		// Timer context
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second) // For Delay
		defer cancel()
	*/

	// Custom options for allocator
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// Robot-like behaviour is blocked by some websites
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, "+
			"like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		chromedp.WindowSize(1920, 1080),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("disable-http2", true),
		// For testing, we can see the browser
		//chromedp.Flag("headless", false), // Set to false to see the browser
	)

	// Setting up allocator context
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	// Create context with the allocator
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// For secure browsing, set timeout
	ctx, cancel = context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	fmt.Printf("Targeting URL: %s\n", rawURL)

	// Enable network events to capture status codes
	var statusCode int64
	var statusText string
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*network.EventResponseReceived); ok {
			// Just capture the main document response
			if ev.Type == network.ResourceTypeDocument {
				statusCode = ev.Response.Status
				statusText = ev.Response.StatusText
			}
		}
	})
	err = chromedp.Run(ctx, chromedp.Navigate(rawURL))
	// print network request status
	listNetworkRequests(statusCode, statusText)
	if err != nil {
		log.Fatal("Failed to navigate: ", err)
	}

	// Navigate to the URL
	err = chromedp.Run(ctx, chromedp.Navigate(rawURL))
	// Handle error
	if err != nil {
		log.Fatal("Failed to navigate: ", err)
	}

	// Run content retrieval
	htmlData, err := contentRetrieval(ctx)

	// Get html content
	if err != nil {
		log.Println("Failed to retrieve content: ", err)
	} else {
		// Save html within the folder
		savePath := filepath.Join(folderPath, "page.html")
		if err := os.WriteFile(savePath, []byte(htmlData), 0644); err != nil {
			log.Println("Failed to save HTML file: ", err)
		} else {
			fmt.Printf("HTML content saved to %s\n", savePath)
		}
	}

	imgData, err := captureScreenshot(ctx)
	if err != nil {
		log.Println("Image fault: ", err)
	} else {
		// Save screenshot within the folder
		savepath := filepath.Join(folderPath, "screenshot.png")
		if err := os.WriteFile(savepath, imgData, 0644); err != nil {
			log.Println("Failed to save screenshot: ", err)
		} else {
			fmt.Printf("Screenshot saved to %s\n", savepath)
		}
	}

	links, err := extractLinks(ctx)
	if err != nil {
		log.Println("Failed to extract links: ", err)
	} else {
		// Save links within the folder
		savepath := filepath.Join(folderPath, "links.txt")
		linksContent := strings.Join(links, "\n")
		if err := os.WriteFile(savepath, []byte(linksContent), 0644); err != nil {
			log.Println("Failed to save links: ", err)
		} else {
			fmt.Printf("Links saved to %d links in %s\n", len(links), savepath)
		}
	}
}

func contentRetrieval(ctx context.Context) (string, error) {
	var htmlContent string

	// Get everything tagged with <html>
	err := chromedp.Run(ctx, chromedp.OuterHTML("html", &htmlContent))
	// Handle error
	if err != nil {
		log.Printf("Error retrieving content: %v", err)
	}

	return htmlContent, err
}

func captureScreenshot(ctx context.Context) ([]byte, error) {
	// The image is formed using zeros and ones.
	var screenShotBuffer []byte

	// Take full page ss
	// Picture quality 0 - 100, we set to 90
	err := chromedp.Run(ctx, chromedp.FullScreenshot(&screenShotBuffer, 90))

	// Handle error
	if err != nil {
		log.Printf("Error capturing screenshot: %v", err)
	}

	return screenShotBuffer, err
}

func extractLinks(ctx context.Context) ([]string, error) {
	var jsonResult string
	// JavaScript to extract all href attributes from <a> tags
	// a little vast because sometimes href is object for SVG links
	javascript := `JSON.stringify(Array.from(document.querySelectorAll('a')).map(a => {
		if (typeof a.href === 'object' && a.href !== null) {
			return a.href.baseVal; // SVG linkleri için
		}
		return a.href; // Normal linkler için
	}).filter(href => typeof href === 'string' && href !== ""))`
	// Evaluate the JavaScript in the page context
	err := chromedp.Run(ctx, chromedp.Evaluate(javascript, &jsonResult))
	if err != nil {
		return nil, fmt.Errorf("error extracting links: %v", err)
	}
	//unpack the JSON string into a Go slice
	var links []string
	err = json.Unmarshal([]byte(jsonResult), &links)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %v", err)
	}
	return links, nil
}

// Network request status code analysis
func listNetworkRequests(code int64, text string) {
	if code == 0 {
		return
	}
	fmt.Printf("Request network: %d (%s)\n", code, text)
	switch {
	case code >= 200 && code < 300:
		fmt.Println("Request SUCCESSFUL: Site is accessible.")
	case code >= 300 && code < 400:
		fmt.Printf("Request REDIRECTION (%d): Site is redirecting to another address.\n", code)
	case code == 403:
		log.Println("Request FORBIDDEN (403): Access denied (WAF or Bot Protection).")
	case code == 404:
		log.Println("Request NOT FOUND (404): Page does not exist.")
	case code >= 400 && code < 500:
		log.Printf("Request CLIENT ERROR (%d): %s\n", code, text)
	case code >= 500:
		log.Printf("Request SERVER ERROR (%d): Target site is down or faulty.\n", code)
	default:
		log.Printf("Request UNKNOWN STATUS: %d\n", code)
	}
}
