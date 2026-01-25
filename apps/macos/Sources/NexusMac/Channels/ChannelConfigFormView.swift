import SwiftUI
import OSLog

/// A dynamic form view that renders channel configuration based on a schema.
struct ChannelConfigFormView: View {
    let schema: ChannelSchema
    @Binding var config: [String: Any]

    var onSave: () -> Void
    var onCancel: () -> Void
    var onTestConnection: (() async -> Result<String, Error>)?

    @State private var validationErrors: [String: String] = [:]
    @State private var isTestingConnection = false
    @State private var testConnectionResult: TestConnectionResult?

    private let logger = Logger(subsystem: "com.nexus.mac", category: "channel-config")

    enum TestConnectionResult {
        case success(String)
        case failure(String)
    }

    var body: some View {
        Form {
            headerSection

            if let sections = schema.sections {
                ForEach(sections) { section in
                    formSection(section)
                }
            } else {
                Section {
                    ForEach(schema.fields) { field in
                        fieldView(for: field)
                    }
                }
            }

            testConnectionSection
            actionSection
        }
        .formStyle(.grouped)
        .onAppear {
            initializeDefaults()
        }
    }

    // MARK: - Header Section

    private var headerSection: some View {
        Section {
            HStack(spacing: 12) {
                Image(systemName: schema.iconName)
                    .font(.largeTitle)
                    .foregroundStyle(.accent)
                    .frame(width: 48, height: 48)
                    .background(Color.accentColor.opacity(0.1))
                    .clipShape(RoundedRectangle(cornerRadius: 10))

                VStack(alignment: .leading, spacing: 4) {
                    Text(schema.displayName)
                        .font(.headline)
                    Text("Configure your \(schema.displayName) integration")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                Spacer()
            }
            .padding(.vertical, 4)
        }
    }

    // MARK: - Form Sections

    @ViewBuilder
    private func formSection(_ section: ChannelSchemaSection) -> some View {
        let sectionFields = schema.fields.filter { section.fieldIds.contains($0.id) }
        if !sectionFields.isEmpty {
            Section(section.title) {
                ForEach(sectionFields) { field in
                    fieldView(for: field)
                }
            }
        }
    }

    // MARK: - Field Views

    @ViewBuilder
    private func fieldView(for field: ChannelField) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            switch field.type {
            case .text:
                textFieldView(for: field)
            case .password:
                passwordFieldView(for: field)
            case .number:
                numberFieldView(for: field)
            case .toggle:
                toggleFieldView(for: field)
            case .select:
                selectFieldView(for: field)
            case .multiSelect:
                multiSelectFieldView(for: field)
            }

            // Help text
            if let helpText = field.helpText {
                Text(helpText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            // Validation error
            if let error = validationErrors[field.id] {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    private func textFieldView(for field: ChannelField) -> some View {
        HStack {
            Text(field.label)
                .frame(width: 120, alignment: .leading)

            TextField(
                field.placeholder ?? "",
                text: stringBinding(for: field.id)
            )
            .textFieldStyle(.roundedBorder)
            .onChange(of: config.getString(field.id)) { _, _ in
                validateField(field)
            }

            if field.required {
                requiredIndicator
            }
        }
    }

    private func passwordFieldView(for field: ChannelField) -> some View {
        HStack {
            Text(field.label)
                .frame(width: 120, alignment: .leading)

            SecureField(
                field.placeholder ?? "",
                text: stringBinding(for: field.id)
            )
            .textFieldStyle(.roundedBorder)
            .onChange(of: config.getString(field.id)) { _, _ in
                validateField(field)
            }

            if field.required {
                requiredIndicator
            }
        }
    }

    private func numberFieldView(for field: ChannelField) -> some View {
        HStack {
            Text(field.label)
                .frame(width: 120, alignment: .leading)

            TextField(
                field.placeholder ?? "",
                text: stringBinding(for: field.id)
            )
            .textFieldStyle(.roundedBorder)
            #if os(iOS)
            .keyboardType(.numberPad)
            #endif
            .onChange(of: config.getString(field.id)) { _, newValue in
                // Filter non-numeric characters
                let filtered = newValue.filter { $0.isNumber || $0 == "-" }
                if filtered != newValue {
                    config[field.id] = filtered
                }
                validateField(field)
            }

            if field.required {
                requiredIndicator
            }
        }
    }

    private func toggleFieldView(for field: ChannelField) -> some View {
        Toggle(field.label, isOn: boolBinding(for: field.id))
            .toggleStyle(.switch)
    }

    private func selectFieldView(for field: ChannelField) -> some View {
        HStack {
            Text(field.label)
                .frame(width: 120, alignment: .leading)

            Picker("", selection: stringBinding(for: field.id)) {
                if !field.required {
                    Text("Select...").tag("")
                }
                ForEach(field.options ?? []) { option in
                    Text(option.label).tag(option.value)
                }
            }
            .labelsHidden()
            .pickerStyle(.menu)
            .frame(maxWidth: .infinity, alignment: .leading)

            if field.required {
                requiredIndicator
            }
        }
    }

    private func multiSelectFieldView(for field: ChannelField) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(field.label)
                .font(.subheadline)

            LazyVGrid(columns: [GridItem(.adaptive(minimum: 150))], alignment: .leading, spacing: 8) {
                ForEach(field.options ?? []) { option in
                    MultiSelectCheckbox(
                        label: option.label,
                        isSelected: multiSelectBinding(for: field.id, option: option.value)
                    )
                }
            }
        }
    }

    private var requiredIndicator: some View {
        Text("*")
            .foregroundStyle(.red)
            .font(.caption)
    }

    // MARK: - Test Connection Section

    @ViewBuilder
    private var testConnectionSection: some View {
        if onTestConnection != nil {
            Section("Connection Test") {
                HStack {
                    Button {
                        Task {
                            await testConnection()
                        }
                    } label: {
                        HStack(spacing: 8) {
                            if isTestingConnection {
                                ProgressView()
                                    .controlSize(.small)
                            }
                            Text("Test Connection")
                        }
                    }
                    .buttonStyle(.bordered)
                    .disabled(isTestingConnection || !isFormValid)

                    Spacer()

                    if let result = testConnectionResult {
                        testResultView(result)
                    }
                }

                if let result = testConnectionResult {
                    switch result {
                    case .success(let message):
                        Text(message)
                            .font(.caption)
                            .foregroundStyle(.green)
                    case .failure(let message):
                        Text(message)
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func testResultView(_ result: TestConnectionResult) -> some View {
        switch result {
        case .success:
            HStack(spacing: 4) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                Text("Connected")
                    .font(.caption)
                    .foregroundStyle(.green)
            }
        case .failure:
            HStack(spacing: 4) {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.red)
                Text("Failed")
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    // MARK: - Action Section

    private var actionSection: some View {
        Section {
            HStack {
                Button("Cancel", role: .cancel) {
                    onCancel()
                }
                .keyboardShortcut(.escape)

                Spacer()

                Button("Save") {
                    if validateAllFields() {
                        onSave()
                    }
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.return, modifiers: .command)
                .disabled(!isFormValid)
            }
        }
    }

    // MARK: - Bindings

    private func stringBinding(for key: String) -> Binding<String> {
        Binding(
            get: { config.getString(key) },
            set: { config[key] = $0 }
        )
    }

    private func boolBinding(for key: String) -> Binding<Bool> {
        Binding(
            get: { config.getBool(key) },
            set: { config[key] = $0 }
        )
    }

    private func multiSelectBinding(for key: String, option: String) -> Binding<Bool> {
        Binding(
            get: {
                config.getStringArray(key).contains(option)
            },
            set: { isSelected in
                var array = config.getStringArray(key)
                if isSelected {
                    if !array.contains(option) {
                        array.append(option)
                    }
                } else {
                    array.removeAll { $0 == option }
                }
                config[key] = array
            }
        )
    }

    // MARK: - Validation

    private var isFormValid: Bool {
        for field in schema.fields where field.required {
            let value = config[field.id]
            if value == nil {
                return false
            }
            if let stringValue = value as? String, stringValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return false
            }
        }
        return validationErrors.isEmpty
    }

    private func validateField(_ field: ChannelField) {
        let value = config[field.id]

        // Check required
        if field.required {
            if value == nil {
                validationErrors[field.id] = "\(field.label) is required"
                return
            }
            if let stringValue = value as? String, stringValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                validationErrors[field.id] = "\(field.label) is required"
                return
            }
        }

        // Check validation rules
        if let validation = field.validation, let error = validation.validate(value) {
            validationErrors[field.id] = error
            return
        }

        // Clear error if valid
        validationErrors.removeValue(forKey: field.id)
    }

    private func validateAllFields() -> Bool {
        validationErrors.removeAll()

        for field in schema.fields {
            validateField(field)
        }

        if !validationErrors.isEmpty {
            logger.warning("form validation failed: \(self.validationErrors.keys.joined(separator: ", "))")
        }

        return validationErrors.isEmpty
    }

    private func initializeDefaults() {
        for field in schema.fields {
            if config[field.id] == nil, let defaultValue = field.defaultValue {
                switch field.type {
                case .toggle:
                    config[field.id] = defaultValue.lowercased() == "true"
                case .number:
                    config[field.id] = defaultValue
                case .multiSelect:
                    config[field.id] = [String]()
                default:
                    config[field.id] = defaultValue
                }
            }
        }
    }

    // MARK: - Test Connection

    private func testConnection() async {
        guard let onTestConnection else { return }

        isTestingConnection = true
        testConnectionResult = nil

        let result = await onTestConnection()

        isTestingConnection = false

        switch result {
        case .success(let message):
            testConnectionResult = .success(message)
            logger.info("connection test succeeded")
        case .failure(let error):
            testConnectionResult = .failure(error.localizedDescription)
            logger.warning("connection test failed: \(error.localizedDescription)")
        }
    }
}

// MARK: - Multi-Select Checkbox

private struct MultiSelectCheckbox: View {
    let label: String
    @Binding var isSelected: Bool

    var body: some View {
        Button {
            isSelected.toggle()
        } label: {
            HStack(spacing: 8) {
                Image(systemName: isSelected ? "checkmark.square.fill" : "square")
                    .foregroundStyle(isSelected ? .accent : .secondary)

                Text(label)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
            }
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Channel Config Sheet

/// A sheet wrapper for the channel configuration form.
struct ChannelConfigSheet: View {
    let channelType: ChannelsStore.Channel.ChannelType
    let existingConfig: [String: Any]?

    var onSave: ([String: Any]) -> Void
    var onCancel: () -> Void
    var onTestConnection: (([String: Any]) async -> Result<String, Error>)?

    @State private var config: [String: Any] = [:]

    private var schema: ChannelSchema {
        ChannelSchema.schema(for: channelType)
    }

    var body: some View {
        ChannelConfigFormView(
            schema: schema,
            config: $config,
            onSave: {
                onSave(config)
            },
            onCancel: onCancel,
            onTestConnection: onTestConnection.map { handler in
                { await handler(config) }
            }
        )
        .frame(width: 500, height: 600)
        .onAppear {
            if let existingConfig {
                config = existingConfig
            }
        }
    }
}

// MARK: - Add Channel View

/// View for selecting and configuring a new channel.
struct AddChannelView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var channels = ChannelsStore.shared

    @State private var selectedType: ChannelsStore.Channel.ChannelType?
    @State private var channelName = ""
    @State private var config: [String: Any] = [:]
    @State private var isSaving = false
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            if let selectedType {
                channelConfigView(for: selectedType)
            } else {
                channelTypeSelectionView
            }
        }
        .frame(width: 550, height: 650)
    }

    // MARK: - Channel Type Selection

    private var channelTypeSelectionView: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Add Channel")
                .font(.title2.bold())
                .padding(.horizontal)

            Text("Select the type of messaging channel to add:")
                .foregroundStyle(.secondary)
                .padding(.horizontal)

            List {
                ForEach(ChannelSchema.all) { schema in
                    Button {
                        selectedType = schema.channelType
                    } label: {
                        HStack(spacing: 12) {
                            Image(systemName: schema.iconName)
                                .font(.title2)
                                .foregroundStyle(.accent)
                                .frame(width: 40)

                            VStack(alignment: .leading, spacing: 2) {
                                Text(schema.displayName)
                                    .font(.headline)
                                    .foregroundStyle(.primary)

                                Text(channelDescription(schema.channelType))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }

                            Spacer()

                            Image(systemName: "chevron.right")
                                .foregroundStyle(.secondary)
                        }
                        .padding(.vertical, 8)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
            .listStyle(.inset)

            HStack {
                Spacer()
                Button("Cancel") {
                    dismiss()
                }
            }
            .padding()
        }
    }

    // MARK: - Channel Config View

    private func channelConfigView(for type: ChannelsStore.Channel.ChannelType) -> some View {
        let schema = ChannelSchema.schema(for: type)

        return VStack(spacing: 0) {
            // Back button and name field
            HStack {
                Button {
                    selectedType = nil
                    config = [:]
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "chevron.left")
                        Text("Back")
                    }
                }
                .buttonStyle(.plain)

                Spacer()
            }
            .padding()

            // Channel name
            HStack {
                Text("Channel Name")
                    .frame(width: 120, alignment: .leading)

                TextField("My \(schema.displayName)", text: $channelName)
                    .textFieldStyle(.roundedBorder)
            }
            .padding(.horizontal)

            // Config form
            ChannelConfigFormView(
                schema: schema,
                config: $config,
                onSave: {
                    Task {
                        await saveChannel(type: type)
                    }
                },
                onCancel: {
                    dismiss()
                },
                onTestConnection: { config in
                    await testConnection(type: type, config: config)
                }
            )

            // Error message
            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding()
            }
        }
        .disabled(isSaving)
        .overlay {
            if isSaving {
                ProgressView("Saving...")
                    .padding()
                    .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8))
            }
        }
    }

    // MARK: - Actions

    private func saveChannel(type: ChannelsStore.Channel.ChannelType) async {
        let name = channelName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else {
            errorMessage = "Please enter a channel name"
            return
        }

        isSaving = true
        errorMessage = nil

        do {
            let channelConfig = ChannelsStore.Channel.ChannelConfig(
                phoneNumber: config["phoneNumber"] as? String,
                botToken: config["botToken"] as? String,
                webhookUrl: config["webhookUrl"] as? String,
                apiKey: config["apiKey"] as? String,
                enabled: config.getBool("enabled")
            )

            let channel = ChannelsStore.Channel(
                id: UUID().uuidString,
                type: type,
                name: name,
                status: .disconnected,
                config: channelConfig,
                lastMessageAt: nil,
                messageCount: 0
            )

            try await channels.addChannel(channel)
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }

        isSaving = false
    }

    private func testConnection(type: ChannelsStore.Channel.ChannelType, config: [String: Any]) async -> Result<String, Error> {
        // This would call the actual test connection API
        // For now, simulate a test
        try? await Task.sleep(nanoseconds: 1_000_000_000)

        // In a real implementation, this would validate credentials
        return .success("Connection successful")
    }

    // MARK: - Helpers

    private func channelDescription(_ type: ChannelsStore.Channel.ChannelType) -> String {
        switch type {
        case .whatsapp:
            return "Connect via WhatsApp Business API"
        case .telegram:
            return "Connect via Telegram Bot API"
        case .slack:
            return "Connect to Slack workspaces"
        case .discord:
            return "Connect to Discord servers"
        case .sms:
            return "Send SMS via Twilio or Vonage"
        case .email:
            return "Send emails via SMTP"
        }
    }
}

// MARK: - Previews

#Preview("Config Form - WhatsApp") {
    ChannelConfigFormView(
        schema: .whatsapp,
        config: .constant([:]),
        onSave: {},
        onCancel: {},
        onTestConnection: { _ in .success("Connected") }
    )
    .frame(width: 500, height: 600)
}

#Preview("Config Form - Email") {
    ChannelConfigFormView(
        schema: .email,
        config: .constant(["smtpPort": "587", "useTLS": true]),
        onSave: {},
        onCancel: {},
        onTestConnection: nil
    )
    .frame(width: 500, height: 600)
}

#Preview("Add Channel") {
    AddChannelView()
}
