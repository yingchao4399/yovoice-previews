using System.IO;
using System.Text;
using System.Text.Json;

namespace VoiceWorkbench.Desktop;

public static class DraftRecovery
{
    // 备份不走业务校验，保留导致保存失败的原始作品，且不改动已有状态文件。
    public static void Save(string path, string json)
    {
        using var document = JsonDocument.Parse(json);
        if (document.RootElement.ValueKind != JsonValueKind.Object) throw new IOException(HostL10n.T("未读取到当前作品，无法备份。", "Could not read the current work to back it up."));
        string temporary = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
        try
        {
            using (var file = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            {
                file.Write(Encoding.UTF8.GetBytes(json));
                file.Flush(true);
            }
            File.Move(temporary, path, true);
        }
        finally { if (File.Exists(temporary)) File.Delete(temporary); }
    }
}
