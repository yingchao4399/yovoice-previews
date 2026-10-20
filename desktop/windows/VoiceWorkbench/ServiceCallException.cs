using System.IO;
using System.Text.Json;

namespace VoiceWorkbench.Desktop;

// 跨过 JsonDocument 生命周期保留原始错误，交给前端按当前语言显示。
public sealed class ServiceCallException : IOException
{
    public const string StorageRestartRequired = "@yovoice.error.storageRestartRequired";
    public JsonElement Wire { get; }
    public string Code => Wire.ValueKind == JsonValueKind.Object && Wire.TryGetProperty("code", out var value) && value.ValueKind == JsonValueKind.String ? value.GetString()! : "";

    public ServiceCallException(JsonElement wire) : base(Describe(wire)) => Wire = wire.Clone();

    private static string Describe(JsonElement wire)
    {
        if (wire.ValueKind == JsonValueKind.String) return wire.GetString() ?? HostL10n.T("请求失败。", "Request failed.");
        if (wire.ValueKind != JsonValueKind.Object) return HostL10n.T("本地服务返回了无效错误：", "The local service returned an invalid error: ") + wire;
        string code = wire.TryGetProperty("code", out var value) && value.ValueKind == JsonValueKind.String ? value.GetString()! : "";
        // 错误文案与网页共用同一份翻译，按当前界面语言选择。
        var assembly = typeof(ServiceCallException).Assembly;
        using var stream = assembly.GetManifestResourceStream($"yovoice.messages.{HostL10n.Locale.ToLowerInvariant()}.json") ?? assembly.GetManifestResourceStream("yovoice.messages.en.json");
        using var catalog = stream is null ? null : JsonDocument.Parse(stream);
        string message = catalog is not null && catalog.RootElement.TryGetProperty(code, out var entry)
            ? entry.GetProperty("defaultMessage").GetString()! : HostL10n.T("本地服务请求失败。", "The local service request failed.");
        if (wire.TryGetProperty("params", out var parameters) && parameters.ValueKind == JsonValueKind.Object)
        {
            foreach (var parameter in parameters.EnumerateObject()) message = message.Replace("{" + parameter.Name + "}", parameter.Value.ToString());
            if (parameters.TryGetProperty("detail", out var detail) && !message.Contains(detail.ToString())) message += "\n" + detail;
        }
        return message + (code.Length > 0 ? "\n" + code : "\n" + wire);
    }
}
