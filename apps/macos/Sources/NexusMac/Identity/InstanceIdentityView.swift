import SwiftUI

/// A view displaying instance identity information with copyable fields.
struct InstanceIdentityView: View {
    @State private var copiedField: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Instance Identity")
                .font(.title2)

            Text("Unique identifiers for this Nexus installation.")
                .font(.caption)
                .foregroundColor(.secondary)

            VStack(alignment: .leading, spacing: 12) {
                IdentityRow(
                    label: "Instance ID",
                    value: InstanceIdentity.instanceId,
                    description: "Stable UUID for this installation",
                    copiedField: $copiedField
                )

                IdentityRow(
                    label: "Display Name",
                    value: InstanceIdentity.displayName,
                    description: "Computer's friendly name",
                    copiedField: $copiedField
                )

                IdentityRow(
                    label: "Model Identifier",
                    value: InstanceIdentity.modelIdentifier ?? "Unknown",
                    description: "Mac hardware model",
                    copiedField: $copiedField
                )

                IdentityRow(
                    label: "Hardware UUID",
                    value: InstanceIdentity.hardwareUUID ?? "Not available",
                    description: "System-level UUID from IOKit",
                    copiedField: $copiedField
                )

                if let serial = InstanceIdentity.serialNumber {
                    IdentityRow(
                        label: "Serial Number",
                        value: serial,
                        description: "Hardware serial number",
                        copiedField: $copiedField
                    )
                }
            }
            .padding(.top, 8)
        }
    }
}

// MARK: - Identity Row

private struct IdentityRow: View {
    let label: String
    let value: String
    let description: String
    @Binding var copiedField: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(label)
                    .font(.headline)
                Spacer()
                Button(action: copyValue) {
                    HStack(spacing: 4) {
                        Image(systemName: copiedField == label ? "checkmark" : "doc.on.doc")
                        Text(copiedField == label ? "Copied" : "Copy")
                    }
                    .font(.caption)
                }
                .buttonStyle(.borderless)
            }

            Text(value)
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(NSColor.controlBackgroundColor))
                .cornerRadius(6)
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.gray.opacity(0.3), lineWidth: 1)
                )

            Text(description)
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }

    private func copyValue() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
        copiedField = label

        // Reset after delay
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            if copiedField == label {
                copiedField = nil
            }
        }
    }
}

// MARK: - Preview

#if DEBUG
struct InstanceIdentityView_Previews: PreviewProvider {
    static var previews: some View {
        InstanceIdentityView()
            .padding()
            .frame(width: 400)
    }
}
#endif
