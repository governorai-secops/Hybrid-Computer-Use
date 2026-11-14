import ApplicationServices
import ComputerUseProviderKit
import CoreGraphics
import Darwin
import Foundation

@main
struct ComputerUseProviderMain {
    static func main() async {
        if CommandLine.arguments.contains("--request-permissions") {
            requestMissingPermissions()
            try? await Task.sleep(nanoseconds: 25_000_000_000)
            Darwin._exit(EXIT_SUCCESS)
        }
        do {
            requestMissingPermissions()
            let transport = try ProviderLineTransport(
                arguments: CommandLine.arguments
            )
            let server = ProviderServer(transport: transport)
            await server.run()
            Darwin._exit(EXIT_SUCCESS)
        } catch {
            fputs("computer-use provider startup failed: \(error)\n", stderr)
            exit(1)
        }
    }

    /// Ask TCC once. The normal runtime checks stay silent; this flag is the prompt.
    static func requestMissingPermissions() {
        if !AXIsProcessTrusted() {
            let options = ["AXTrustedCheckOptionPrompt": true] as CFDictionary
            _ = AXIsProcessTrustedWithOptions(options)
        }
        if !CGPreflightPostEventAccess() {
            _ = CGRequestPostEventAccess()
        }
        if !CGPreflightScreenCaptureAccess() {
            _ = CGRequestScreenCaptureAccess()
        }
    }
}
