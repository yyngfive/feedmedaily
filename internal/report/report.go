package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

// WriteLatestJSON persists latest.json plus the date-stamped JSON snapshot.
func WriteLatestJSON(report store.Report, reportsDir string) (string, error) {
	dataDir := filepath.Join(reportsDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", fmt.Errorf("create reports data dir: %w", err)
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode report json: %w", err)
	}
	latestPath := filepath.Join(dataDir, "latest.json")
	datedPath := filepath.Join(dataDir, report.ReportDate+".json")
	for _, path := range []string{latestPath, datedPath} {
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			return "", fmt.Errorf("write report json %s: %w", path, err)
		}
	}
	return latestPath, nil
}

// PublishStaticApp mirrors the Python static-report publishing flow.
func PublishStaticApp(webDistDir string, reportsDir string, report store.Report) (string, error) {
	targetDir := filepath.Join(reportsDir, "latest")
	if dirExists(webDistDir) {
		_ = os.RemoveAll(targetDir)
		if err := copyDir(webDistDir, targetDir); err != nil {
			return "", fmt.Errorf("copy web dist: %w", err)
		}
		if err := writeEmbeddedReportScript(report, targetDir); err != nil {
			return "", err
		}
		indexPath := filepath.Join(targetDir, "index.html")
		if err := patchIndexForEmbeddedData(indexPath); err != nil {
			return "", err
		}
		if err := inlineStaticAssets(indexPath); err != nil {
			return "", err
		}
		return indexPath, nil
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create static report dir: %w", err)
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode fallback report html: %w", err)
	}
	indexPath := filepath.Join(targetDir, "index.html")
	html := strings.TrimSpace(`
<!doctype html>
<meta charset="utf-8">
<title>FeedMeDaily</title>
<body>
  <h1>FeedMeDaily report data generated</h1>
  <p>Build the bundled web assets before publishing this static dashboard.</p>
  <pre id="report-json">` + string(payload) + `</pre>
</body>`)
	if err := os.WriteFile(indexPath, []byte(html), 0o644); err != nil {
		return "", fmt.Errorf("write fallback static report: %w", err)
	}
	return indexPath, nil
}

func writeEmbeddedReportScript(report store.Report, targetDir string) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode embedded report script: %w", err)
	}
	script := "window.__SCIRSS_REPORT__ = " + string(payload) + ";"
	path := filepath.Join(targetDir, "report-data.js")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return fmt.Errorf("write embedded report script: %w", err)
	}
	return nil
}

func patchIndexForEmbeddedData(indexPath string) error {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("read static index: %w", err)
	}
	text := string(data)
	if strings.Contains(text, "report-data.js") {
		return nil
	}
	marker := `<script type="module"`
	if strings.Contains(text, marker) {
		text = strings.Replace(text, marker, `<script src="./report-data.js"></script>`+"\n    "+marker, 1)
	} else {
		text = strings.Replace(text, "</head>", `  <script src="./report-data.js"></script>`+"\n  </head>", 1)
	}
	if err := os.WriteFile(indexPath, []byte(text), 0o644); err != nil {
		return fmt.Errorf("patch static index: %w", err)
	}
	return nil
}

func inlineStaticAssets(indexPath string) error {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("read index for asset inlining: %w", err)
	}
	text := string(data)
	root := filepath.Dir(indexPath)

	linkRe := regexp.MustCompile(`<link rel="stylesheet"[^>]*href="([^"]+)">`)
	if match := linkRe.FindStringSubmatch(text); len(match) == 2 {
		cssPath := filepath.Join(root, filepath.FromSlash(match[1]))
		cssData, err := os.ReadFile(cssPath)
		if err != nil {
			return fmt.Errorf("read bundled css: %w", err)
		}
		text = strings.Replace(text, match[0], "<style>\n"+string(cssData)+"\n</style>", 1)
	}

	scriptRe := regexp.MustCompile(`<script type="module"[^>]*src="([^"]+)"></script>`)
	if match := scriptRe.FindStringSubmatch(text); len(match) == 2 {
		jsPath := filepath.Join(root, filepath.FromSlash(match[1]))
		jsData, err := os.ReadFile(jsPath)
		if err != nil {
			return fmt.Errorf("read bundled js: %w", err)
		}
		text = strings.Replace(text, match[0], "<script type=\"module\">\n"+string(jsData)+"\n</script>", 1)
	}

	reportDataPath := filepath.Join(root, "report-data.js")
	if reportData, err := os.ReadFile(reportDataPath); err == nil {
		text = strings.Replace(text, `<script src="./report-data.js"></script>`, "<script>\n"+string(reportData)+"\n</script>", 1)
	}

	if err := os.WriteFile(indexPath, []byte(text), 0o644); err != nil {
		return fmt.Errorf("write inlined static index: %w", err)
	}
	return nil
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src string, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
