using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text.Json;

namespace VoiceWorkbench.Desktop;

// 原生位置使用工作区坐标和像素，不能与 WPF 的逻辑尺寸直接混用。
[StructLayout(LayoutKind.Sequential)]
public struct WindowPlacement
{
    public int Length;
    public int Flags;
    public int ShowCommand;
    public int MinX, MinY, MaxX, MaxY;
    public int Left, Top, Right, Bottom;

    private static readonly JsonSerializerOptions JsonOptions = new() { IncludeFields = true };

    public static WindowPlacement Capture(IntPtr handle)
    {
        var placement = new WindowPlacement { Length = Marshal.SizeOf<WindowPlacement>() };
        if (!GetWindowPlacement(handle, ref placement)) throw new Win32Exception();
        return placement;
    }

    public static WindowPlacement? Load(string path)
    {
        try
        {
            var placement = JsonSerializer.Deserialize<WindowPlacement>(File.ReadAllText(path), JsonOptions);
            return placement.Right > placement.Left && placement.Bottom > placement.Top ? placement : null;
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or JsonException)
        {
            Debug.WriteLine($"未读取窗口位置，使用默认布局：{error.Message}");
            return null;
        }
    }

    public void Save(string path)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(path)!);
            File.WriteAllText(path + ".tmp", JsonSerializer.Serialize(this, JsonOptions));
            File.Move(path + ".tmp", path, true);
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException)
        {
            // 布局保存失败不应阻止用户关闭窗口或保存作品。
            Debug.WriteLine($"未保存窗口位置：{error.Message}");
        }
    }

    public void Fit(int left, int top, int width, int height)
    {
        int fittedWidth = (int)Math.Clamp((long)Right - Left, 1, width);
        int fittedHeight = (int)Math.Clamp((long)Bottom - Top, 1, height);
        Left = Math.Clamp(Left, left, left + width - fittedWidth);
        Top = Math.Clamp(Top, top, top + height - fittedHeight);
        Right = Left + fittedWidth;
        Bottom = Top + fittedHeight;
    }

    public void Apply(IntPtr handle)
    {
        PrepareRestore();
        if (!SetWindowPlacement(handle, ref this)) throw new Win32Exception();
    }

    public void PrepareRestore()
    {
        Length = Marshal.SizeOf<WindowPlacement>();
        // 最小化不是下次启动状态，但必须保留最小化之前的最大化状态。
        ShowCommand = ShowCommand == 3 || (ShowCommand == 2 && (Flags & 2) != 0) ? 3 : 1;
        Flags = 0;
    }

    [DllImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetWindowPlacement(IntPtr handle, ref WindowPlacement placement);

    [DllImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool SetWindowPlacement(IntPtr handle, ref WindowPlacement placement);
}
