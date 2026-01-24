import SwiftUI

struct ProvidersView: View {
    @EnvironmentObject var model: AppModel
    @State private var showQR = false
    @State private var qrProviderName: String = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Providers")
                    .font(.title2)
                Spacer()
                Button("Refresh") {
                    Task { await model.refreshProviders() }
                }
            }

            Table(model.providers) {
                TableColumn("Name") { Text($0.name) }
                TableColumn("Enabled") { Text($0.enabled ? "Yes" : "No") }
                TableColumn("Connected") { Text($0.connected ? "Yes" : "No") }
                TableColumn("Healthy") { Text(($0.healthy ?? false) ? "Yes" : "No") }
                TableColumn("QR") { provider in
                    if provider.qrAvailable ?? false {
                        Button("Show") {
                            qrProviderName = provider.name
                            Task {
                                await model.loadProviderQR(name: provider.name)
                                showQR = true
                            }
                        }
                    } else {
                        Text("-")
                    }
                }
            }

            if let error = model.lastError {
                Text(error)
                    .foregroundColor(.red)
            }

            Spacer()
        }
        .padding()
        .sheet(isPresented: $showQR) {
            ProviderQRSheet(providerName: qrProviderName)
                .environmentObject(model)
        }
    }
}

struct ProviderQRSheet: View {
    @EnvironmentObject var model: AppModel
    let providerName: String

    var body: some View {
        VStack(spacing: 16) {
            Text("QR Code: \(providerName)")
                .font(.headline)
            if let image = model.providerQRImages[providerName] {
                Image(nsImage: image)
                    .interpolation(.none)
                    .resizable()
                    .scaledToFit()
                    .frame(maxWidth: 320, maxHeight: 320)
            } else {
                ProgressView("Loading...")
            }
        }
        .padding()
        .frame(minWidth: 360, minHeight: 420)
    }
}
