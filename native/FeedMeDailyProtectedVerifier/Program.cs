using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace FeedMeDailyProtectedVerifier;

internal static class Program
{
    [STAThread]
    private static void Main(string[] args)
    {
        var options = CliOptions.Parse(args);
        ApplicationConfiguration.Initialize();
        Application.Run(new VerificationForm(options));
    }
}

internal sealed class CliOptions
{
    public required string VerificationId { get; init; }
    public required string JobId { get; init; }
    public required string VerificationHost { get; init; }
    public required string CallbackUrl { get; init; }
    public required string UserDataDir { get; init; }
    public required string LogsDir { get; init; }
    public required string AppVersion { get; init; }
    public required List<string> FeedUrls { get; init; }

    public static CliOptions Parse(string[] args)
    {
        string? verificationId = null;
        string? jobId = null;
        string? verificationHost = null;
        string? callbackUrl = null;
        string? userDataDir = null;
        string? logsDir = null;
        string? appVersion = null;
        var feedUrls = new List<string>();

        for (var index = 0; index < args.Length; index++)
        {
            if (index + 1 >= args.Length)
            {
                continue;
            }

            switch (args[index])
            {
                case "--verification-id":
                    verificationId = args[++index];
                    break;
                case "--job-id":
                    jobId = args[++index];
                    break;
                case "--verification-host":
                    verificationHost = args[++index];
                    break;
                case "--callback-url":
                    callbackUrl = args[++index];
                    break;
                case "--user-data-dir":
                    userDataDir = args[++index];
                    break;
                case "--logs-dir":
                    logsDir = args[++index];
                    break;
                case "--app-version":
                    appVersion = args[++index];
                    break;
                case "--feed-url":
                    feedUrls.Add(args[++index]);
                    break;
            }
        }

        if (string.IsNullOrWhiteSpace(verificationId) ||
            string.IsNullOrWhiteSpace(callbackUrl) ||
            string.IsNullOrWhiteSpace(userDataDir) ||
            string.IsNullOrWhiteSpace(verificationHost) ||
            feedUrls.Count == 0)
        {
            throw new InvalidOperationException("verification-id, verification-host, callback-url, user-data-dir, and at least one feed-url are required");
        }

        return new CliOptions
        {
            VerificationId = verificationId.Trim(),
            JobId = (jobId ?? string.Empty).Trim(),
            VerificationHost = verificationHost.Trim(),
            CallbackUrl = callbackUrl.Trim(),
            UserDataDir = userDataDir.Trim(),
            LogsDir = (logsDir ?? string.Empty).Trim(),
            AppVersion = (appVersion ?? string.Empty).Trim(),
            FeedUrls = feedUrls.Where(item => !string.IsNullOrWhiteSpace(item)).Select(item => item.Trim()).Distinct(StringComparer.OrdinalIgnoreCase).ToList(),
        };
    }
}

internal sealed class VerificationForm : Form
{
    private static readonly HttpClient Http = new() { Timeout = TimeSpan.FromSeconds(15) };

    private readonly CliOptions _options;
    private readonly WebView2 _webView;
    private readonly Label _statusLabel;
    private readonly string _logPath;
    private readonly System.Windows.Forms.Timer _needsUserTimer;
    private readonly Queue<string> _remainingFeedUrls;
    private readonly Dictionary<string, CapturedFeed> _capturedFeeds = new(StringComparer.OrdinalIgnoreCase);
    private bool _completionPosted;
    private bool _needsUserPosted;
    private string? _currentFeedUrl;

    public VerificationForm(CliOptions options)
    {
        _options = options;
        _remainingFeedUrls = new Queue<string>(options.FeedUrls);
        _logPath = BuildLogPath(options.LogsDir);
        Text = "Protected Feed Verification";
        Width = 1240;
        Height = 920;
        StartPosition = FormStartPosition.CenterScreen;

        _statusLabel = new Label
        {
            Dock = DockStyle.Top,
            Height = 64,
            Padding = new Padding(14, 12, 14, 12),
            Text = "FeedMeDaily is opening protected feeds in a persistent WebView2 profile. If Cloudflare asks for a human check, complete it here and leave the window open while the remaining feeds load.",
        };
        _webView = new WebView2 { Dock = DockStyle.Fill };
        _needsUserTimer = new System.Windows.Forms.Timer { Interval = 60_000 };
        _needsUserTimer.Tick += OnNeedsUserTimerTick;

        Controls.Add(_webView);
        Controls.Add(_statusLabel);
        Log($"started verification_id={_options.VerificationId} host={_options.VerificationHost} feeds={_options.FeedUrls.Count}");
    }

    protected override async void OnShown(EventArgs e)
    {
        base.OnShown(e);
        try
        {
            Directory.CreateDirectory(_options.UserDataDir);
            var environment = await CoreWebView2Environment.CreateAsync(userDataFolder: _options.UserDataDir);
            await _webView.EnsureCoreWebView2Async(environment);
            _webView.CoreWebView2.Settings.IsStatusBarEnabled = false;
            _webView.CoreWebView2.Settings.AreDefaultContextMenusEnabled = false;
            _webView.CoreWebView2.Settings.AreDevToolsEnabled = false;
            _webView.CoreWebView2.NavigationCompleted += OnNavigationCompleted;
            _webView.CoreWebView2.WebResourceResponseReceived += OnWebResourceResponseReceived;
            _needsUserTimer.Start();
            NavigateNextFeed();
        }
        catch (Exception ex)
        {
            Log($"startup failed: {ex.Message}");
            await PostTerminalResultAsync("failed", false, ex.Message);
            Close();
        }
    }

    protected override async void OnFormClosing(FormClosingEventArgs e)
    {
        if (!_completionPosted)
        {
            Log("window closed before completion");
            await PostTerminalResultAsync("aborted", _capturedFeeds.Count > 0, "the protected-feed verification window was closed before all feed XML was captured");
        }
        _needsUserTimer.Stop();
        base.OnFormClosing(e);
    }

    private void NavigateNextFeed()
    {
        if (_remainingFeedUrls.Count == 0)
        {
            _ = CompleteAndCloseAsync();
            return;
        }

        _currentFeedUrl = _remainingFeedUrls.Dequeue();
        _statusLabel.Text = $"Opening protected feed {_capturedFeeds.Count + 1}/{_options.FeedUrls.Count}. If Cloudflare appears, complete the check and keep this window open.";
        Log($"navigate feed={_currentFeedUrl}");
        _webView.Source = new Uri(_currentFeedUrl);
    }

    private void OnNavigationCompleted(object? sender, CoreWebView2NavigationCompletedEventArgs e)
    {
        if (_completionPosted)
        {
            return;
        }

        if (e.IsSuccess)
        {
            Log($"navigation completed feed={_currentFeedUrl}");
            _statusLabel.Text = _needsUserPosted
                ? "Cloudflare approval received. FeedMeDaily is now collecting the remaining protected-feed XML documents."
                : "Checking whether this protected feed now resolves to XML.";
        }
        else
        {
            Log($"navigation failed feed={_currentFeedUrl} status={e.WebErrorStatus}");
            _statusLabel.Text = "The page has not fully loaded yet. If Cloudflare appears, complete the human verification and keep the window open.";
        }
    }

    private async void OnNeedsUserTimerTick(object? sender, EventArgs e)
    {
        if (_completionPosted || _needsUserPosted)
        {
            _needsUserTimer.Stop();
            return;
        }

        _needsUserPosted = true;
        _needsUserTimer.Stop();
        _statusLabel.Text = "FeedMeDaily has not captured XML yet. If this page is asking for verification, complete it here or use the browser fallback in the app.";
        Log($"needs_user watchdog fired feed={_currentFeedUrl}");
        await PostNeedsUserAsync();
    }

    private async void OnWebResourceResponseReceived(object? sender, CoreWebView2WebResourceResponseReceivedEventArgs e)
    {
        if (_completionPosted || string.IsNullOrWhiteSpace(_currentFeedUrl))
        {
            return;
        }

        var requestUri = e.Request.Uri?.Trim();
        if (string.IsNullOrWhiteSpace(requestUri))
        {
            return;
        }

        if (!string.Equals(requestUri, _currentFeedUrl, StringComparison.OrdinalIgnoreCase))
        {
            return;
        }

        try
        {
            var response = e.Response;
            var contentType = HeaderValue(response, "Content-Type");
            await using var stream = await response.GetContentAsync();
            using var reader = new StreamReader(stream, Encoding.UTF8);
            var body = await reader.ReadToEndAsync();
            Log($"response feed={_currentFeedUrl} content_type={contentType} bytes={body.Length}");

            if (LooksLikeXml(contentType, body))
            {
                _capturedFeeds[_currentFeedUrl] = new CapturedFeed
                {
                    FeedUrl = _currentFeedUrl,
                    ContentType = contentType,
                    FeedXml = body,
                };

                _statusLabel.Text = $"Captured {_capturedFeeds.Count}/{_options.FeedUrls.Count} protected-feed XML documents.";
                Log($"captured xml feed={_currentFeedUrl} captured={_capturedFeeds.Count}/{_options.FeedUrls.Count}");
                BeginInvoke(new Action(NavigateNextFeed));
                return;
            }

            if (LooksLikeChallenge(contentType, body))
            {
                _statusLabel.Text = "Cloudflare still needs a human check in this window. Complete it once and FeedMeDaily will keep trying the remaining protected feeds automatically.";
                Log($"challenge detected feed={_currentFeedUrl}");
                if (!_needsUserPosted)
                {
                    _needsUserPosted = true;
                    _needsUserTimer.Stop();
                    await PostNeedsUserAsync();
                }
            }
        }
        catch (Exception ex)
        {
            Log($"response handling failed: {ex.Message}");
            await PostTerminalResultAsync("failed", _capturedFeeds.Count > 0, ex.Message);
            BeginInvoke(new Action(Close));
        }
    }

    private async Task CompleteAndCloseAsync()
    {
        if (_completionPosted)
        {
            return;
        }

        if (_capturedFeeds.Count == 0)
        {
            Log("complete without captured xml");
            await PostTerminalResultAsync("failed", false, "the protected-feed verifier did not capture any feed XML");
            Close();
            return;
        }

        Log($"complete captured={_capturedFeeds.Count}");
        await PostTerminalResultAsync("success", true, string.Empty);
        BeginInvoke(new Action(Close));
    }

    private async Task PostNeedsUserAsync()
    {
        var payload = new CallbackPayload
        {
            VerificationId = _options.VerificationId,
            VerificationHost = _options.VerificationHost,
            FeedUrl = _currentFeedUrl ?? string.Empty,
            Status = "needs_user",
            Error = string.Empty,
            SessionVerified = false,
            CapturedFeeds = new List<CapturedFeed>(),
        };
        Log($"post needs_user feed={payload.FeedUrl}");
        await PostPayloadAsync(payload);
    }

    private async Task PostTerminalResultAsync(string status, bool sessionVerified, string error)
    {
        if (_completionPosted)
        {
            return;
        }

        _completionPosted = true;
        _needsUserTimer.Stop();
        var payload = new CallbackPayload
        {
            VerificationId = _options.VerificationId,
            VerificationHost = _options.VerificationHost,
            FeedUrl = _currentFeedUrl ?? string.Empty,
            Status = status,
            Error = error,
            SessionVerified = sessionVerified,
            CapturedFeeds = _capturedFeeds.Values.OrderBy(item => item.FeedUrl, StringComparer.OrdinalIgnoreCase).ToList(),
        };
        Log($"post terminal status={status} session_verified={sessionVerified} captured={payload.CapturedFeeds.Count} error={error}");
        await PostPayloadAsync(payload);
    }

    private async Task PostPayloadAsync(CallbackPayload payload)
    {
        using var content = new StringContent(JsonSerializer.Serialize(payload), Encoding.UTF8, "application/json");
        try
        {
            using var response = await Http.PostAsync(_options.CallbackUrl, content);
            Log($"callback status={(int)response.StatusCode} reason={response.ReasonPhrase}");
        }
        catch (Exception ex)
        {
            Log($"callback failed: {ex.Message}");
        }
    }

    private static string BuildLogPath(string logsDir)
    {
        if (string.IsNullOrWhiteSpace(logsDir))
        {
            return string.Empty;
        }
        return Path.Combine(logsDir.Trim(), "protected-verifier", DateTime.Now.ToString("yyyy-MM-dd") + ".log");
    }

    private void Log(string message)
    {
        if (string.IsNullOrWhiteSpace(_logPath))
        {
            return;
        }
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(_logPath)!);
            File.AppendAllText(_logPath, $"{DateTime.Now:yyyy-MM-dd HH:mm:ss.fff} {_options.VerificationId} {message}{Environment.NewLine}", Encoding.UTF8);
        }
        catch
        {
        }
    }

    private static string HeaderValue(CoreWebView2WebResourceResponseView response, string name)
    {
        if (response.Headers.Contains(name))
        {
            return response.Headers.GetHeader(name) ?? string.Empty;
        }
        return string.Empty;
    }

    private static bool LooksLikeXml(string? contentType, string body)
    {
        var loweredType = (contentType ?? string.Empty).ToLowerInvariant();
        if (loweredType.Contains("xml"))
        {
            return true;
        }

        var trimmed = body.TrimStart();
        return trimmed.StartsWith("<?xml", StringComparison.OrdinalIgnoreCase)
            || trimmed.StartsWith("<rdf:RDF", StringComparison.OrdinalIgnoreCase)
            || trimmed.StartsWith("<rss", StringComparison.OrdinalIgnoreCase)
            || trimmed.StartsWith("<feed", StringComparison.OrdinalIgnoreCase);
    }

    private static bool LooksLikeChallenge(string? contentType, string body)
    {
        var loweredType = (contentType ?? string.Empty).ToLowerInvariant();
        if (!loweredType.Contains("html"))
        {
            return false;
        }

        var sample = body.ToLowerInvariant();
        return sample.Contains("just a moment")
            || sample.Contains("enable javascript and cookies")
            || sample.Contains("cf-browser-verification")
            || sample.Contains("__cf_chl_")
            || sample.Contains("challenge-platform");
    }

    private sealed class CallbackPayload
    {
        [JsonPropertyName("verification_id")]
        public required string VerificationId { get; init; }
        [JsonPropertyName("verification_host")]
        public required string VerificationHost { get; init; }
        [JsonPropertyName("feed_url")]
        public required string FeedUrl { get; init; }
        [JsonPropertyName("status")]
        public required string Status { get; init; }
        [JsonPropertyName("content_type")]
        public string ContentType { get; init; } = "application/xml";
        [JsonPropertyName("feed_xml")]
        public string FeedXml { get; init; } = string.Empty;
        [JsonPropertyName("error")]
        public required string Error { get; init; }
        [JsonPropertyName("session_verified")]
        public required bool SessionVerified { get; init; }
        [JsonPropertyName("captured_feeds")]
        public required List<CapturedFeed> CapturedFeeds { get; init; }
    }

    private sealed class CapturedFeed
    {
        [JsonPropertyName("feed_url")]
        public required string FeedUrl { get; init; }
        [JsonPropertyName("content_type")]
        public string ContentType { get; init; } = "application/xml";
        [JsonPropertyName("feed_xml")]
        public required string FeedXml { get; init; }
    }
}
