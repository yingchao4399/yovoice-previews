using System.Threading;
using System.Windows;

namespace VoiceWorkbench.Desktop;
public partial class App : Application
{
    public const string RelaunchArgument = "--relaunch";
    private Mutex? instance;
    private bool owned;
    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        instance = new Mutex(false, "Local\\VoiceWorkbench.Desktop");
        // 自动重启时旧实例仍在保存和关停服务，等待其释放单实例锁，而不是提示“已在运行”。
        var wait = e.Args.Contains(RelaunchArgument) ? TimeSpan.FromSeconds(60) : TimeSpan.Zero;
        try { owned = instance.WaitOne(wait); }
        catch (AbandonedMutexException) { owned = true; }
        if (!owned) { AppDialog.Show(null, HostL10n.T("yovoice 已在运行", "yovoice is already running"), HostL10n.T("请点击系统托盘中的 yovoice 图标打开窗口。", "Click the yovoice icon in the system tray to open the window.")); Shutdown(); return; }
        try { new MainWindow().Show(); }
        catch (OperationCanceledException) { Shutdown(); }
        catch (Exception error) { AppDialog.Show(null, HostL10n.T("无法启动 yovoice", "Could not start yovoice"), error.Message); Shutdown(1); }
    }
    protected override void OnExit(ExitEventArgs e)
    {
        if (owned) instance?.ReleaseMutex();
        instance?.Dispose();
        base.OnExit(e);
    }
}
