using System.Runtime.InteropServices;
using VoiceWorkbench.Desktop;

static void Require(bool condition, string message)
{
    if (!condition) throw new Exception(message);
}

Require(Marshal.SizeOf<WindowPlacement>() == 44, "WINDOWPLACEMENT 必须匹配 Win32 布局。");
var unchanged = new WindowPlacement { Left = 200, Top = 100, Right = 1640, Bottom = 1020 };
unchanged.Fit(0, 0, 2560, 1400);
Require(unchanged.Left == 200 && unchanged.Top == 100 && unchanged.Right == 1640 && unchanged.Bottom == 1020, "大屏不应改变用户保存的大小和位置。");
// 1080p、150% 缩放下，默认 1440×920 的逻辑尺寸超出可用工作区。
var placement = new WindowPlacement { Left = -120, Top = -200, Right = 2040, Bottom = 1180 };
placement.Fit(0, 0, 1920, 1040);
Require(placement.Left == 0 && placement.Top == 0 && placement.Right == 1920 && placement.Bottom == 1040, "高缩放下窗口必须完整可见。");
placement = new WindowPlacement { Left = 2500, Top = 500, Right = 3500, Bottom = 1300 };
placement.Fit(0, 0, 1366, 728);
Require(placement.Left == 366 && placement.Top == 0 && placement.Bottom == 728, "拔掉副屏后必须移回可用区域。");
placement.Fit(-1920, 0, 1920, 1040);
Require(placement.Left == -1000 && placement.Right == 0, "支持负坐标副屏。");
foreach (var state in new[] { (2, 2, 3), (2, 0, 1), (3, 0, 3), (1, 0, 1) })
{
    placement.ShowCommand = state.Item1;
    placement.Flags = state.Item2;
    placement.PrepareRestore();
    Require(placement.ShowCommand == state.Item3 && placement.Flags == 0, "恢复时应保留最大化、清除最小化。");
}
string directory = Path.Combine(Path.GetTempPath(), "yovoice-window-check-" + Guid.NewGuid().ToString("N"));
string path = Path.Combine(directory, "window-placement.json");
try
{
    Require(WindowPlacement.Load(path) is null, "首次启动应使用默认布局。");
    placement.ShowCommand = 3;
    placement.Save(path);
    Require(WindowPlacement.Load(path) is { } saved && saved.Left == placement.Left && saved.Right == placement.Right && saved.ShowCommand == placement.ShowCommand, "窗口状态应能保存并恢复。");
    foreach (string invalid in new[] { "{broken", "{}", "{\"Left\":100,\"Right\":50}" })
    {
        File.WriteAllText(path, invalid);
        Require(WindowPlacement.Load(path) is null, "损坏的布局不能阻止启动。");
    }
}
finally { Directory.Delete(directory, true); }
Console.WriteLine("窗口尺寸适配、屏幕越界、最大化恢复及布局文件检查通过。");
