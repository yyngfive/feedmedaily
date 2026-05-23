//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type verifierApp struct {
	cfg        verifierConfig
	ctx        context.Context
	httpClient *http.Client
	successURL string
	successSrv *http.Server
	pollCancel context.CancelFunc
	reported   atomic.Bool
}

func newVerifierApp(cfg verifierConfig) (*verifierApp, error) {
	app := &verifierApp{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
	if err := app.startSuccessServer(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *verifierApp) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.WindowSetTitle(ctx, fmt.Sprintf("Feed Verification - %s", feedLabel(a.cfg.FeedURL)))
	runtime.WindowShow(ctx)
	runtime.WindowUnminimise(ctx)
	runtime.WindowCenter(ctx)
	runtime.WindowSetAlwaysOnTop(ctx, true)
	pollCtx, cancel := context.WithCancel(ctx)
	a.pollCancel = cancel
	go a.pollForCapturedXML(pollCtx)
}

func (a *verifierApp) shutdown(ctx context.Context) {
	if a.pollCancel != nil {
		a.pollCancel()
	}
	if a.successSrv != nil {
		_ = a.successSrv.Shutdown(ctx)
	}
	if strings.TrimSpace(a.cfg.UserDataDir) != "" {
		_ = osRemoveAll(a.cfg.UserDataDir)
	}
}

func (a *verifierApp) beforeClose(ctx context.Context) bool {
	if !a.reported.Load() {
		_ = a.reportFailure("the verification window was closed before RSS XML was captured")
	}
	return false
}

func (a *verifierApp) startSuccessServer() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start verifier local callback server: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mark-success", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.reported.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	a.successSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	a.successURL = "http://" + listener.Addr().String() + "/mark-success"
	go func() {
		_ = a.successSrv.Serve(listener)
	}()
	return nil
}

func (a *verifierApp) pollForCapturedXML(ctx context.Context) {
	timer := time.NewTicker(1200 * time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if a.reported.Load() {
				return
			}
			runtime.WindowExecJS(a.ctx, detectionScript(a.cfg.VerificationID, a.cfg.CallbackURL, a.successURL))
		}
	}
}

func (a *verifierApp) reportFailure(message string) error {
	if a.reported.Load() {
		return nil
	}
	return a.postCallback("aborted", "", "", message)
}

func (a *verifierApp) postCallback(status string, contentType string, feedXML string, errorText string) error {
	payload := map[string]string{
		"verification_id": a.cfg.VerificationID,
		"status":          status,
		"content_type":    contentType,
		"feed_xml":        feedXML,
		"error":           errorText,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, a.cfg.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := a.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("callback returned %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (a *verifierApp) assetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(introHTML(a.cfg.FeedURL)))
	})
}

func introHTML(feedURL string) string {
	label := html.EscapeString(feedLabel(feedURL))
	urlText := html.EscapeString(feedURL)
	quotedFeedURL, _ := json.Marshal(feedURL)
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Feed Verification</title>
  <style>
    :root { color-scheme: light; }
    body {
      margin: 0;
      font-family: "Segoe UI", "Microsoft YaHei UI", sans-serif;
      background: linear-gradient(180deg, #f7efe4 0%, #fbf8f2 100%);
      color: #1f2933;
    }
    main {
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 32px;
    }
    .card {
      width: min(760px, 100%);
      background: rgba(255, 255, 255, 0.94);
      border: 1px solid rgba(214, 177, 122, 0.35);
      border-radius: 20px;
      padding: 28px 30px;
      box-shadow: 0 26px 60px rgba(84, 62, 28, 0.14);
    }
    h1 {
      margin: 0 0 10px;
      font-size: 28px;
      line-height: 1.2;
    }
    p {
      margin: 0;
      line-height: 1.7;
      color: #52606d;
    }
    .url {
      margin-top: 16px;
      padding: 14px 16px;
      border-radius: 14px;
      background: #fff9ef;
      border: 1px solid rgba(214, 177, 122, 0.45);
      color: #6b4f1d;
      word-break: break-all;
      font-size: 14px;
    }
    .actions {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      margin-top: 24px;
    }
    button {
      border: 0;
      border-radius: 999px;
      padding: 12px 18px;
      font-size: 15px;
      font-weight: 600;
      cursor: pointer;
      background: #1f4b99;
      color: #fff;
      box-shadow: 0 10px 24px rgba(31, 75, 153, 0.22);
    }
    .secondary {
      background: transparent;
      color: #1f2933;
      border: 1px solid rgba(82, 96, 109, 0.25);
      box-shadow: none;
    }
    .hint {
      margin-top: 20px;
      font-size: 13px;
      color: #7b8794;
    }
  </style>
</head>
<body>
  <main>
    <section class="card">
      <h1>Finish the protected feed check for ` + label + `</h1>
      <p>
        This window uses its own WebView2 session. If Cloudflare asks for a human check,
        complete it here and wait until the protected feed page opens. The app will keep
        probing this window and capture the XML automatically once it becomes available.
      </p>
      <div class="url">` + urlText + `</div>
      <div class="actions">
        <button id="open-now">Open protected feed now</button>
        <button class="secondary" id="close-window">Cancel</button>
      </div>
      <p class="hint">
        The feed will open automatically in about 1 second. If the XML appears directly,
        leave this window open for a moment so the app can capture it.
      </p>
    </section>
  </main>
  <script>
    const feedUrl = ` + string(quotedFeedURL) + `;
    const openFeed = () => window.location.replace(feedUrl);
    document.getElementById("open-now").addEventListener("click", openFeed);
    document.getElementById("close-window").addEventListener("click", () => window.close());
    window.setTimeout(openFeed, 1200);
  </script>
</body>
</html>`
}

func detectionScript(verificationID string, callbackURL string, successURL string) string {
	quotedVerificationID, _ := json.Marshal(verificationID)
	quotedCallbackURL, _ := json.Marshal(callbackURL)
	quotedSuccessURL, _ := json.Marshal(successURL)
	return `(async () => {
  if (window.__feedVerificationPosted) {
    return;
  }
  const root = document && document.documentElement;
  if (!root) {
    return;
  }
  const rootName = String(root.tagName || root.nodeName || "").toLowerCase();
  const contentType = String(document.contentType || "").toLowerCase();
  const looksLikeXML =
    contentType.includes("xml") ||
    rootName === "rss" ||
    rootName === "feed" ||
    rootName === "rdf:rdf" ||
    rootName === "rdf";
  const decodeHtml = (value) => {
    if (!value) {
      return "";
    }
    const textarea = document.createElement("textarea");
    textarea.innerHTML = value;
    return textarea.value || "";
  };
  const extractXMLText = (candidate) => {
    if (!candidate) {
      return "";
    }
    const sources = [String(candidate), decodeHtml(String(candidate))];
    const prefixes = ["<?xml", "<rss", "<feed", "<rdf:rdf", "<rdf:RDF"];
    for (const source of sources) {
      const trimmed = source.trim();
      if (!trimmed) {
        continue;
      }
      const lower = trimmed.toLowerCase();
      let start = -1;
      for (const prefix of prefixes) {
        const index = lower.indexOf(prefix.toLowerCase());
        if (index >= 0 && (start === -1 || index < start)) {
          start = index;
        }
      }
      if (start < 0) {
        continue;
      }
      const snippet = trimmed.slice(start).trim();
      if (snippet.length < 32) {
        continue;
      }
      if (
        snippet.includes("<item") ||
        snippet.includes("<entry") ||
        snippet.includes("<channel") ||
        snippet.includes("<rdf:RDF") ||
        snippet.includes("<rdf:rdf")
      ) {
        return snippet;
      }
    }
    return "";
  };
  let xml = "";
  try {
    if (looksLikeXML) {
      xml = new XMLSerializer().serializeToString(document);
    }
  } catch (_error) {
    xml = root.outerHTML || "";
  }
  if (!xml || xml.length < 32) {
    xml = extractXMLText(document.body && (document.body.innerText || document.body.textContent || ""));
  }
  if ((!xml || xml.length < 32) && document.documentElement) {
    xml = extractXMLText(document.documentElement.outerHTML || "");
  }
  if (!xml || xml.length < 32) {
    return;
  }
  window.__feedVerificationPosted = true;
  const payload = {
    verification_id: ` + string(quotedVerificationID) + `,
    status: "success",
    content_type: looksLikeXML ? (document.contentType || "application/xml") : "application/xml",
    feed_xml: xml,
    error: ""
  };
  try {
    const callbackResponse = await fetch(` + string(quotedCallbackURL) + `, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload)
    });
    if (!callbackResponse.ok) {
      throw new Error("callback returned " + callbackResponse.status);
    }
    await fetch(` + string(quotedSuccessURL) + `, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: "{}"
    });
    document.open();
    document.write("<!doctype html><html><head><meta charset='utf-8'><title>Verification captured</title><style>body{font-family:'Segoe UI','Microsoft YaHei UI',sans-serif;background:#fbf8f2;color:#1f2933;display:grid;place-items:center;min-height:100vh;margin:0;padding:32px;}section{max-width:640px;border:1px solid rgba(214,177,122,.35);background:#fff;border-radius:20px;padding:28px 30px;box-shadow:0 24px 52px rgba(84,62,28,.12);}h1{margin:0 0 12px;font-size:28px;}p{margin:0;line-height:1.7;color:#52606d;}</style></head><body><section><h1>Feed XML captured</h1><p>You can return to FeedMeDaily now and click Continue After Verification. This window can be closed.</p></section></body></html>");
    document.close();
  } catch (error) {
    window.__feedVerificationPosted = false;
    console.error("Feed verification callback failed:", error);
  }
})();`
}

func feedLabel(feedURL string) string {
	if host, err := urlHost(feedURL); err == nil && host != "" {
		return host
	}
	return "Protected feed"
}

func urlHost(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(parsed.Hostname()), nil
}

var osRemoveAll = func(path string) error {
	return os.RemoveAll(path)
}
