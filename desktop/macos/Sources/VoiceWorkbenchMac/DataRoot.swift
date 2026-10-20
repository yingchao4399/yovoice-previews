import AppKit

/// 解析数据目录：环境变量优先，其次是“设置 › 存储”写入的重定向配置，最后回退到 ~/.yovoice。
/// 重定向配置只由 Go 服务解析（--data-root），宿主不直接读写 storage.json。
enum DataRoot {
    static func resolve() -> URL {
        let environment = ProcessInfo.processInfo.environment
        if let path = environment["WORKBENCH_DATA"], !path.isEmpty {
            return URL(fileURLWithPath: path, isDirectory: true)
        }
        if let configured = configured() { return configured }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".yovoice", isDirectory: true)
    }

    /// “设置 › 存储”选择的数据目录；未自定义时为 nil。
    static func configured() -> URL? { configuration.url }

    /// 存储配置只由服务解析（yovoice-service --data-root），宿主不再各自读取 storage.json；
    /// 配置损坏时记录服务给出的错误，由 confirmAvailable 提示后退出，而不是静默回退到 ~/.yovoice。
    private static let configuration: (url: URL?, error: String?) = {
        guard let resources = Bundle.main.resourceURL else { return (nil, nil) }
        let executable = resources.appendingPathComponent("service/yovoice-service")
        guard FileManager.default.isExecutableFile(atPath: executable.path) else { return (nil, nil) }
        let process = Process()
        process.executableURL = executable
        process.arguments = ["--data-root"]
        let output = Pipe(), errors = Pipe()
        process.standardOutput = output
        process.standardError = errors
        do { try process.run() } catch { return (nil, error.localizedDescription) }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        let message = errors.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else {
            return (nil, String(decoding: message, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines))
        }
        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let path = json["dataPath"] as? String, !path.isEmpty else { return (nil, nil) }
        return (URL(fileURLWithPath: path, isDirectory: true), nil)
    }()

    /// 自定义的数据目录不存在时（外置盘未连接、目录被移动或删除）不能静默新建空目录：
    /// 用户会以为数据全丢了，外置盘未挂载时还会把数据写到系统盘的挂载点上。
    /// 让用户选择重试、退出，或明确在原位置新建空的数据目录。
    @MainActor static func confirmAvailable(_ url: URL) -> URL {
        if ProcessInfo.processInfo.environment["WORKBENCH_DATA"]?.isEmpty ?? true, let message = configuration.error {
            let alert = NSAlert()
            alert.alertStyle = .critical
            alert.messageText = HostL10n.t("alert.storageConfigBroken.title")
            alert.informativeText = message
            alert.addButton(withTitle: HostL10n.t("alert.dataRootMissing.quit"))
            alert.runModal()
            exit(1)
        }
        guard ProcessInfo.processInfo.environment["WORKBENCH_DATA"]?.isEmpty ?? true,
              let configured = configured(), configured.standardizedFileURL.path == url.standardizedFileURL.path else { return url }
        while true {
            var isDirectory: ObjCBool = false
            if FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory), isDirectory.boolValue { return url }
            let alert = NSAlert()
            alert.alertStyle = .warning
            alert.messageText = HostL10n.t("alert.dataRootMissing.title")
            alert.informativeText = HostL10n.t("alert.dataRootMissing.body", url.path)
            alert.addButton(withTitle: HostL10n.t("alert.dataRootMissing.retry"))
            alert.addButton(withTitle: HostL10n.t("alert.dataRootMissing.quit"))
            alert.addButton(withTitle: HostL10n.t("alert.dataRootMissing.create"))
            switch alert.runModal() {
            case .alertFirstButtonReturn:
                continue
            case .alertThirdButtonReturn:
                do {
                    try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
                    return url
                } catch {
                    let failed = NSAlert()
                    failed.messageText = HostL10n.t("alert.dataRootMissing.createFailed")
                    failed.informativeText = error.localizedDescription
                    failed.runModal()
                }
            default:
                exit(0)
            }
        }
    }
}
