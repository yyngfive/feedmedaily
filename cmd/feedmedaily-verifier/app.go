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
	cfg             verifierConfig
	ctx             context.Context
	httpClient      *http.Client
	successURL      string
	eventURL        string
	controlSrv      *http.Server
	pollCancel      context.CancelFunc
	reported        atomic.Bool
	closing         atomic.Bool
	beforeCloseSeen atomic.Bool
	shutdownSeen    atomic.Bool
	logger          *verifierLogger
}

type localEventPayload struct {
	Level   string         `json:"level"`
	Action  string         `json:"action"`
	Message string         `json:"message"`
	Error   string         `json:"error"`
	Data    map[string]any `json:"data"`
}

func newVerifierApp(cfg verifierConfig) (*verifierApp, error) {
	app := &verifierApp{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
		logger: newVerifierLogger(cfg),
	}
	if err := app.startControlServer(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *verifierApp) startup(ctx context.Context) {
	a.ctx = ctx
	a.logger.info("startup_context_ready", "Verifier startup context is ready.", nil)
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
	a.shutdownSeen.Store(true)
	a.logger.info("shutdown_fired", "Verifier shutdown hook fired.", nil)
	if a.pollCancel != nil {
		a.pollCancel()
	}
	if a.controlSrv != nil {
		_ = a.controlSrv.Shutdown(ctx)
	}
	if a.cfg.CleanupProfile && strings.TrimSpace(a.cfg.UserDataDir) != "" {
		_ = osRemoveAll(a.cfg.UserDataDir)
	}
}

func (a *verifierApp) beforeClose(ctx context.Context) bool {
	a.beforeCloseSeen.Store(true)
	a.logger.info("before_close_fired", "Verifier before-close hook fired.", nil)
	if !a.reported.Load() {
		_ = a.reportFailure("the verification window was closed before RSS XML was captured")
	}
	return false
}

func (a *verifierApp) requestClose(reason string) {
	if a.ctx == nil {
		a.logger.warning("close_skipped", "Close requested before the verifier context was ready.", "context is not ready", map[string]any{
			"reason": reason,
		})
		return
	}
	if a.closing.Swap(true) {
		a.logger.info("close_signal_ignored", "Verifier already entered the close phase; ignoring the extra signal.", map[string]any{
			"reason": reason,
		})
		return
	}
	a.logger.info("close_requested", "Verifier entered the close phase after a successful XML callback.", map[string]any{
		"reason": reason,
	})
	if a.pollCancel != nil {
		a.pollCancel()
	}
	go func() {
		runtime.WindowSetAlwaysOnTop(a.ctx, false)
		a.logger.info("close_prepare_window", "Removed AlwaysOnTop before requesting verifier window close.", nil)
		closed := false
		if closer, ok := a.ctx.Value("frontend").(interface{ WindowClose() }); ok {
			closer.WindowClose()
			closed = true
		}
		if closed {
			a.logger.info("close_api_invoked", "Called the verifier window close API.", nil)
		} else {
			a.logger.warning("close_api_unavailable", "The verifier window close API was not available on this Wails context.", "frontend WindowClose was unavailable", nil)
		}
		time.Sleep(100 * time.Millisecond)
		runtime.WindowHide(a.ctx)
		a.logger.info("close_window_hidden", "Hid the verifier window after requesting close.", nil)

		time.Sleep(2 * time.Second)
		if !a.beforeCloseSeen.Load() && !a.shutdownSeen.Load() {
			a.logger.warning("close_timeout", "The verifier close phase timed out before beforeClose/shutdown fired.", "the verifier window did not exit within the expected timeout", nil)
			runtime.Quit(a.ctx)
			time.Sleep(400 * time.Millisecond)
			if !a.shutdownSeen.Load() {
				a.logger.warning("force_exit", "Verifier is forcing the process to exit after the close timeout elapsed.", "the verifier process stayed alive after close timeout", nil)
				osExit(0)
			}
		}
	}()
}

func (a *verifierApp) startControlServer() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start verifier local callback server: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mark-success", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			writeControlHeaders(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeControlHeaders(w)
		a.reported.Store(true)
		a.logger.info("local_success_ack_received", "Verifier received the local success acknowledgment and will begin the close phase.", nil)
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"ok":              true,
			"verification_id": a.cfg.VerificationID,
			"close_phase":     true,
		})
		go a.requestClose("local_success_ack")
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			writeControlHeaders(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeControlHeaders(w)
		var payload localEventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			a.logger.warning("local_event_decode_failed", "Verifier could not decode a local diagnostic event payload.", err.Error(), nil)
			writeJSONResponse(w, http.StatusBadRequest, map[string]any{"ok": false})
			return
		}
		level := strings.ToLower(strings.TrimSpace(payload.Level))
		switch level {
		case "warning":
			a.logger.warning(payload.Action, payload.Message, payload.Error, payload.Data)
		case "error":
			a.logger.error(payload.Action, payload.Message, payload.Error, payload.Data)
		default:
			a.logger.info(payload.Action, payload.Message, payload.Data)
		}
		writeJSONResponse(w, http.StatusOK, map[string]any{"ok": true})
	})
	a.controlSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	baseURL := "http://" + listener.Addr().String()
	a.successURL = baseURL + "/mark-success"
	a.eventURL = baseURL + "/event"
	go func() {
		_ = a.controlSrv.Serve(listener)
	}()
	return nil
}

func (a *verifierApp) pollForCapturedXML(ctx context.Context) {
	timer := time.NewTicker(1200 * time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.info("polling_stopped", "Stopped polling the verifier window for protected feed XML.", nil)
			return
		case <-timer.C:
			if a.reported.Load() {
				a.logger.info("polling_stopped_after_report", "Protected feed XML was already reported; polling loop is stopping.", nil)
				return
			}
			runtime.WindowExecJS(a.ctx, detectionScript(a.cfg.VerificationID, a.cfg.CallbackURL, a.successURL, a.eventURL))
		}
	}
}

func (a *verifierApp) reportFailure(message string) error {
	if a.reported.Load() {
		return nil
	}
	a.logger.warning("backend_callback_post_started", "Posting an aborted verifier result back to the main FeedMeDaily backend.", message, nil)
	if err := a.postCallback("aborted", "", "", message); err != nil {
		a.logger.error("backend_callback_post_failed", "Verifier failed to post the aborted result back to the backend.", err.Error(), nil)
		return err
	}
	a.logger.info("backend_callback_post_succeeded", "Verifier posted the aborted result back to the backend.", map[string]any{
		"status": "aborted",
	})
	return nil
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
		_, _ = w.Write([]byte(introHTML(a.cfg.FeedURL, a.eventURL)))
	})
}

func introHTML(feedURL string, eventURL string) string {
	label := html.EscapeString(feedLabel(feedURL))
	urlText := html.EscapeString(feedURL)
	quotedFeedURL, _ := json.Marshal(feedURL)
	quotedEventURL, _ := json.Marshal(eventURL)
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
        This window uses a dedicated WebView2 browser profile for this publisher. If Cloudflare asks for a human check,
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
    const localEventURL = ` + string(quotedEventURL) + `;
    const localEvent = async (action, level = "info", errorText = "", data = null, message = "") => {
      try {
        await fetch(localEventURL, {
          method: "POST",
          headers: {"Content-Type": "application/json"},
          body: JSON.stringify({
            action,
            level,
            error: errorText,
            message,
            data: data || {}
          })
        });
      } catch (_error) {}
    };
    const openFeed = async () => {
      await localEvent("feed_page_navigation_started", "info", "", {feed_url: feedUrl}, "Verifier is navigating to the protected feed page.");
      window.location.replace(feedUrl);
    };
    document.getElementById("open-now").addEventListener("click", openFeed);
    document.getElementById("close-window").addEventListener("click", () => window.close());
    localEvent("intro_page_loaded", "info", "", {feed_url: feedUrl}, "Verifier intro page loaded.");
    window.setTimeout(openFeed, 1200);
  </script>
</body>
</html>`
}

func detectionScript(verificationID string, callbackURL string, successURL string, eventURL string) string {
	quotedVerificationID, _ := json.Marshal(verificationID)
	quotedCallbackURL, _ := json.Marshal(callbackURL)
	quotedSuccessURL, _ := json.Marshal(successURL)
	quotedEventURL, _ := json.Marshal(eventURL)
	return `(async () => {
  if (window.__feedVerificationPosted) {
    return;
  }
  const localEventURL = ` + string(quotedEventURL) + `;
  const localEvent = async (action, level = "info", errorText = "", data = null, message = "") => {
    try {
      await fetch(localEventURL, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({
          action,
          level,
          error: errorText,
          message,
          data: data || {}
        })
      });
    } catch (_error) {}
  };
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
  await localEvent("xml_detected", "info", "", {
    root_name: rootName,
    content_type: document.contentType || "",
    xml_length: xml.length
  }, "Verifier detected protected feed XML in the current window.");
  const payload = {
    verification_id: ` + string(quotedVerificationID) + `,
    status: "success",
    content_type: looksLikeXML ? (document.contentType || "application/xml") : "application/xml",
    feed_xml: xml,
    error: ""
  };
  let callbackSucceeded = false;
  try {
    await localEvent("backend_callback_post_started", "info", "", {
      content_type: payload.content_type,
      xml_length: xml.length
    }, "Posting captured protected feed XML back to the main backend.");
    const callbackResponse = await fetch(` + string(quotedCallbackURL) + `, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload)
    });
    if (!callbackResponse.ok) {
      throw new Error("callback returned " + callbackResponse.status);
    }
    const callbackAck = await callbackResponse.json().catch(() => ({}));
    callbackSucceeded = true;
    await localEvent("backend_callback_post_succeeded", "info", "", {
      acknowledged: !!callbackAck.acknowledged,
      close_window: !!callbackAck.close_window,
      duplicate: !!callbackAck.duplicate
    }, "Main backend acknowledged the captured protected feed XML.");
    await localEvent("backend_callback_ack_received", "info", "", callbackAck || {}, "Verifier received the backend callback acknowledgment and can enter the close phase.");
    await localEvent("local_success_ack_post_started", "info", "", null, "Posting the local success acknowledgment so the Go close coordinator can take over.");
    const successResponse = await fetch(` + string(quotedSuccessURL) + `, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: "{}"
    });
    if (!successResponse.ok) {
      throw new Error("local success ack returned " + successResponse.status);
    }
    await localEvent("local_success_ack_post_succeeded", "info", "", null, "Verifier local success acknowledgment completed.");
  } catch (error) {
    if (!callbackSucceeded) {
      window.__feedVerificationPosted = false;
      await localEvent("backend_callback_post_failed", "warning", String(error), null, "Verifier failed to post captured XML back to the main backend.");
    } else {
      await localEvent("local_success_ack_post_failed", "warning", String(error), null, "Verifier captured XML and reached the backend, but the local close acknowledgment failed.");
    }
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

func writeControlHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

func writeJSONResponse(w http.ResponseWriter, status int, payload map[string]any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

var osRemoveAll = func(path string) error {
	return os.RemoveAll(path)
}

var osExit = func(code int) {
	os.Exit(code)
}
