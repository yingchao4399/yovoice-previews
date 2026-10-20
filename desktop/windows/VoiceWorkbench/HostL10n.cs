using System.Globalization;
using System.IO;
using System.Text.Json;

namespace VoiceWorkbench.Desktop;

// 原生菜单、对话框与错误提示的界面语言，跟随设置中的 preferences.uiLocale（与网页一致），
// 尚未设置时按系统语言选择。调用处同时写出中英文，便于对照维护。
public static class HostL10n
{
    public static bool English { get; private set; } = !CultureInfo.CurrentUICulture.Name.StartsWith("zh", StringComparison.OrdinalIgnoreCase);
    // 语言切换后刷新已经显示的菜单与按钮文字。
    public static event Action? Changed;

    public static string T(string zh, string en) => English ? en : zh;

    // 读取数据目录中保存的界面语言：服务把它写在 host-preferences.json；
    // 尚未被新版服务打开过的数据目录仍读旧版 state.json，没有该字段时与服务端一致视为中文。
    public static void LoadFrom(string root)
    {
        try
        {
            string path = Path.Combine(root, "host-preferences.json");
            if (!File.Exists(path)) path = Path.Combine(root, "state.json");
            if (!File.Exists(path)) return;
            using var document = JsonDocument.Parse(File.ReadAllText(path));
            if (!document.RootElement.TryGetProperty("preferences", out var preferences) || preferences.ValueKind != JsonValueKind.Object) return;
            Apply(preferences.TryGetProperty("uiLocale", out var locale) && locale.ValueKind == JsonValueKind.String ? locale.GetString() : "zh-CN");
        }
        catch (Exception error) when (error is IOException or JsonException or UnauthorizedAccessException) { }
    }

    // 网页的界面语言；服务错误按它选择翻译目录，原生文案只备中英两份，中文以外的语言用英文。
    public static string Locale { get; private set; } = English ? "en" : "zh-CN";

    public static void Apply(string? uiLocale)
    {
        if (string.IsNullOrEmpty(uiLocale) || Locale == uiLocale) return;
        Locale = uiLocale;
        bool next = !uiLocale.StartsWith("zh", StringComparison.Ordinal);
        if (next == English) return;
        English = next;
        Changed?.Invoke();
    }
}
