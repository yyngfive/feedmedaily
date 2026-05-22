using System.Text;
using System.Text.Json;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace ChemRxivVerifier;

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
    public required string FeedUrl { get; init; }
    public required string CallbackUrl { get; init; }

    public static CliOptions Parse(string[] args)
    {
        string? verificationId = null;
        string? feedUrl = null;
        string? callbackUrl = null;

        for (var index = 0; index < args.Length; index++)
        {
            switch (args[index])
            {
                case "--verification-id" when index + 1 < args.Length:
                    verificationId = args[++index];
                    break;
                case "--feed-url" when index + 1 < args.Length:
                    feedUrl = args[++index];
                    break;
                case "--callback-url" when index + 1 < args.Length:
                    callbackUrl = args[++index];
                    break;
            }
        }

        if (string.IsNullOrWhiteSpace(verificationId) || string.IsNullOrWhiteSpace(feedUrl) || string.IsNullOrWhiteSpace(callbackUrl))
        {
            throw new InvalidOperationException("verification-id, feed-url, and callback-url are required");
        }

        return new CliOptions
        {
            VerificationId = verificationId,
            FeedUrl = feedUrl,
            CallbackUrl = callbackUrl,
        };
    }
}

internal sealed class VerificationForm : Form
{
    private static readonly HttpClient Http = new();
    private readonly CliOptions _options;
    private readonly WebView2 _webView;
    private readonly Label _statusLabel;
    private bool _callbackPosted;

    public VerificationForm(CliOptions options)
    {
        _options = options;
        Text = "ChemRxiv Verification";
        Width = 1200;
        Height = 900;
        StartPosition = FormStartPosition.CenterScreen;

        _statusLabel = new Label
        {
            Dock = DockStyle.Top,
            Height = 56,
            Padding = new Padding(14, 12, 14, 12),
            Text = "Complete the ChemRxiv Cloudflare check in this window. The sync will resume automatically once the RSS XML is captured.",
        };
        _webView = new WebView2
        {
            Dock = DockStyle.Fill,
        };

        Controls.Add(_webView);
        Controls.Add(_statusLabel);
    }

    protected override async void OnShown(EventArgs e)
    {
        base.OnShown(e);
        try
        {
            await _webView.EnsureCoreWebView2Async();
            _webView.CoreWebView2.Settings.IsStatusBarEnabled = false;
            _webView.CoreWebView2.Settings.AreDevToolsEnabled = false;
            _webView.CoreWebView2.NavigationCompleted += OnNavigationCompleted;
            _webView.CoreWebView2.WebResourceResponseReceived += OnWebResourceResponseReceived;
            _webView.Source = new Uri(_options.FeedUrl);
        }
        catch (Exception ex)
        {
            await PostResultAsync("failed", null, null, ex.Message);
            Close();
        }
    }

    protected override async void OnFormClosing(FormClosingEventArgs e)
    {
        if (!_callbackPosted)
        {
            await PostResultAsync("aborted", null, null, null);
        }
        base.OnFormClosing(e);
    }

    private void OnNavigationCompleted(object? sender, CoreWebView2NavigationCompletedEventArgs e)
    {
        if (_callbackPosted)
        {
            return;
        }
        _statusLabel.Text = e.IsSuccess
            ? "Waiting for ChemRxiv to return the XML feed from this verified browser session."
            : "The page did not finish loading yet. Complete the verification and wait for the feed to reopen.";
    }

    private async void OnWebResourceResponseReceived(object? sender, CoreWebView2WebResourceResponseReceivedEventArgs e)
    {
        if (_callbackPosted || !IsTargetFeedResponse(e.Request.Uri))
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
            if (LooksLikeXml(contentType, body))
            {
                _statusLabel.Text = "Verification succeeded. Returning the RDF/XML feed to FeedMeDaily.";
                await PostResultAsync("success", contentType, body, null);
                BeginInvoke(Close);
                return;
            }

            if (LooksLikeChallenge(contentType, body))
            {
                _statusLabel.Text = "Complete the verification in this window, then wait for ChemRxiv to reopen the feed.";
                return;
            }

            _statusLabel.Text = "ChemRxiv returned a non-XML page. Complete the verification and wait for the feed to reopen.";
        }
        catch (Exception ex)
        {
            await PostResultAsync("failed", null, null, ex.Message);
            BeginInvoke(Close);
        }
    }

    private bool IsTargetFeedResponse(string? requestUri)
    {
        if (string.IsNullOrWhiteSpace(requestUri))
        {
            return false;
        }
        return string.Equals(requestUri.Trim(), _options.FeedUrl.Trim(), StringComparison.OrdinalIgnoreCase);
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
            || sample.Contains("__cf_chl_");
    }

    private async Task PostResultAsync(string status, string? contentType, string? feedXml, string? error)
    {
        if (_callbackPosted)
        {
            return;
        }
        _callbackPosted = true;
        var payload = new Dictionary<string, object?>
        {
            ["verification_id"] = _options.VerificationId,
            ["status"] = status,
            ["content_type"] = contentType,
            ["feed_xml"] = feedXml,
            ["error"] = error,
        };
        using var content = new StringContent(JsonSerializer.Serialize(payload), Encoding.UTF8, "application/json");
        try
        {
            await Http.PostAsync(_options.CallbackUrl, content);
        }
        catch
        {
            // Best effort callback; if the local server is gone there is nothing meaningful to do here.
        }
    }
}
