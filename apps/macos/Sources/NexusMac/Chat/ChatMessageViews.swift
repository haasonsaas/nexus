import SwiftUI

/// Bubble shape for chat messages with optional tail
struct ChatBubbleShape: Shape, InsettableShape {
    let isFromUser: Bool
    let cornerRadius: CGFloat
    let tailSize: CGFloat

    var insetAmount: CGFloat = 0

    init(isFromUser: Bool, cornerRadius: CGFloat = 18, tailSize: CGFloat = 8) {
        self.isFromUser = isFromUser
        self.cornerRadius = cornerRadius
        self.tailSize = tailSize
    }

    func path(in rect: CGRect) -> Path {
        let insetRect = rect.insetBy(dx: insetAmount, dy: insetAmount)
        var path = Path()

        let radius = cornerRadius
        let tailWidth = tailSize
        let tailHeight = tailSize * 1.5

        if isFromUser {
            // User bubble - tail on right
            path.move(to: CGPoint(x: insetRect.minX + radius, y: insetRect.minY))
            path.addLine(to: CGPoint(x: insetRect.maxX - radius, y: insetRect.minY))
            path.addArc(center: CGPoint(x: insetRect.maxX - radius, y: insetRect.minY + radius),
                       radius: radius, startAngle: .degrees(-90), endAngle: .degrees(0), clockwise: false)
            path.addLine(to: CGPoint(x: insetRect.maxX, y: insetRect.maxY - radius - tailHeight))

            // Tail
            path.addLine(to: CGPoint(x: insetRect.maxX + tailWidth, y: insetRect.maxY - tailHeight))
            path.addLine(to: CGPoint(x: insetRect.maxX, y: insetRect.maxY))

            path.addLine(to: CGPoint(x: insetRect.minX + radius, y: insetRect.maxY))
            path.addArc(center: CGPoint(x: insetRect.minX + radius, y: insetRect.maxY - radius),
                       radius: radius, startAngle: .degrees(90), endAngle: .degrees(180), clockwise: false)
            path.addLine(to: CGPoint(x: insetRect.minX, y: insetRect.minY + radius))
            path.addArc(center: CGPoint(x: insetRect.minX + radius, y: insetRect.minY + radius),
                       radius: radius, startAngle: .degrees(180), endAngle: .degrees(270), clockwise: false)
        } else {
            // Assistant bubble - tail on left
            path.move(to: CGPoint(x: insetRect.minX + radius, y: insetRect.minY))
            path.addLine(to: CGPoint(x: insetRect.maxX - radius, y: insetRect.minY))
            path.addArc(center: CGPoint(x: insetRect.maxX - radius, y: insetRect.minY + radius),
                       radius: radius, startAngle: .degrees(-90), endAngle: .degrees(0), clockwise: false)
            path.addLine(to: CGPoint(x: insetRect.maxX, y: insetRect.maxY - radius))
            path.addArc(center: CGPoint(x: insetRect.maxX - radius, y: insetRect.maxY - radius),
                       radius: radius, startAngle: .degrees(0), endAngle: .degrees(90), clockwise: false)
            path.addLine(to: CGPoint(x: insetRect.minX + radius, y: insetRect.maxY))

            // Tail
            path.addLine(to: CGPoint(x: insetRect.minX, y: insetRect.maxY))
            path.addLine(to: CGPoint(x: insetRect.minX - tailWidth, y: insetRect.maxY - tailHeight))
            path.addLine(to: CGPoint(x: insetRect.minX, y: insetRect.maxY - radius - tailHeight))

            path.addLine(to: CGPoint(x: insetRect.minX, y: insetRect.minY + radius))
            path.addArc(center: CGPoint(x: insetRect.minX + radius, y: insetRect.minY + radius),
                       radius: radius, startAngle: .degrees(180), endAngle: .degrees(270), clockwise: false)
        }

        path.closeSubpath()
        return path
    }

    func inset(by amount: CGFloat) -> ChatBubbleShape {
        var shape = self
        shape.insetAmount = amount
        return shape
    }
}

/// Individual chat message bubble
struct ChatMessageBubble: View {
    let message: ChatMessage
    let maxWidth: CGFloat
    let showTail: Bool

    init(message: ChatMessage, maxWidth: CGFloat = 560, showTail: Bool = true) {
        self.message = message
        self.maxWidth = maxWidth
        self.showTail = showTail
    }

    private var isFromUser: Bool {
        message.role == .user
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: 8) {
            if isFromUser { Spacer(minLength: 40) }

            VStack(alignment: isFromUser ? .trailing : .leading, spacing: 4) {
                // Message content
                messageContent
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(bubbleBackground)
                    .frame(maxWidth: maxWidth, alignment: isFromUser ? .trailing : .leading)

                // Timestamp
                if let timestamp = formattedTime {
                    Text(timestamp)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 4)
                }
            }

            if !isFromUser { Spacer(minLength: 40) }
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 3)
    }

    @ViewBuilder
    private var messageContent: some View {
        switch message.role {
        case .user:
            Text(message.content)
                .foregroundStyle(.white)
                .textSelection(.enabled)
        case .assistant:
            ChatMarkdownView(content: message.content)
                .textSelection(.enabled)
        case .system:
            Text(message.content)
                .font(.caption)
                .foregroundStyle(.secondary)
                .italic()
        case .tool:
            ToolResultView(content: message.content)
        }
    }

    @ViewBuilder
    private var bubbleBackground: some View {
        if showTail {
            ChatBubbleShape(isFromUser: isFromUser)
                .fill(backgroundColor)
        } else {
            RoundedRectangle(cornerRadius: 18)
                .fill(backgroundColor)
        }
    }

    private var backgroundColor: Color {
        switch message.role {
        case .user:
            return .accentColor
        case .assistant:
            return Color(nsColor: .controlBackgroundColor)
        case .system:
            return Color.secondary.opacity(0.1)
        case .tool:
            return Color.secondary.opacity(0.15)
        }
    }

    private var formattedTime: String? {
        let formatter = DateFormatter()
        formatter.timeStyle = .short
        return formatter.string(from: message.timestamp)
    }
}

/// Tool result display
struct ToolResultView: View {
    let content: String
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Image(systemName: "hammer")
                    .font(.caption)
                Text("Tool Result")
                    .font(.caption.weight(.medium))
                Spacer()
                Button {
                    withAnimation { isExpanded.toggle() }
                } label: {
                    Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                        .font(.caption)
                }
                .buttonStyle(.plain)
            }
            .foregroundStyle(.secondary)

            if isExpanded || content.count < 200 {
                Text(content)
                    .font(.system(.caption, design: .monospaced))
                    .textSelection(.enabled)
            } else {
                Text(content.prefix(200) + "...")
                    .font(.system(.caption, design: .monospaced))
            }
        }
    }
}

/// Streaming message indicator
struct StreamingIndicator: View {
    @State private var opacity: Double = 0.3

    var body: some View {
        HStack(spacing: 4) {
            ForEach(0..<3, id: \.self) { index in
                Circle()
                    .fill(Color.accentColor)
                    .frame(width: 6, height: 6)
                    .opacity(opacity)
                    .animation(
                        .easeInOut(duration: 0.5)
                        .repeatForever()
                        .delay(Double(index) * 0.15),
                        value: opacity
                    )
            }
        }
        .onAppear {
            opacity = 1.0
        }
    }
}

/// Message list for chat
struct ChatMessageList: View {
    let messages: [ChatMessage]
    let streamingText: String?

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 6) {
                    ForEach(messages) { message in
                        ChatMessageBubble(
                            message: message,
                            showTail: shouldShowTail(for: message)
                        )
                        .id(message.id)
                    }

                    // Streaming message
                    if let streaming = streamingText, !streaming.isEmpty {
                        streamingBubble(streaming)
                            .id("streaming")
                    }
                }
                .padding(.vertical, 8)
            }
            .onChange(of: messages.count) { _, _ in
                scrollToBottom(proxy)
            }
            .onChange(of: streamingText) { _, _ in
                scrollToBottom(proxy)
            }
        }
    }

    private func shouldShowTail(for message: ChatMessage) -> Bool {
        guard let index = messages.firstIndex(where: { $0.id == message.id }) else {
            return true
        }

        // Show tail if this is the last message or next message is from different role
        if index == messages.count - 1 {
            return true
        }

        return messages[index + 1].role != message.role
    }

    @ViewBuilder
    private func streamingBubble(_ text: String) -> some View {
        HStack(alignment: .bottom, spacing: 8) {
            VStack(alignment: .leading, spacing: 4) {
                ChatMarkdownView(content: text)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(
                        RoundedRectangle(cornerRadius: 18)
                            .fill(Color(nsColor: .controlBackgroundColor))
                    )

                StreamingIndicator()
                    .padding(.leading, 14)
            }
            Spacer(minLength: 40)
        }
        .padding(.horizontal, 6)
    }

    private func scrollToBottom(_ proxy: ScrollViewProxy) {
        withAnimation(.easeOut(duration: 0.2)) {
            if streamingText != nil && !streamingText!.isEmpty {
                proxy.scrollTo("streaming", anchor: .bottom)
            } else if let lastMessage = messages.last {
                proxy.scrollTo(lastMessage.id, anchor: .bottom)
            }
        }
    }
}

#Preview {
    ChatMessageList(
        messages: [
            ChatMessage(id: "1", sessionId: "s1", role: .user, content: "Hello!", timestamp: Date(), metadata: nil),
            ChatMessage(id: "2", sessionId: "s1", role: .assistant, content: "Hi there! How can I help you today?", timestamp: Date(), metadata: nil),
            ChatMessage(id: "3", sessionId: "s1", role: .user, content: "Can you explain Swift concurrency?", timestamp: Date(), metadata: nil)
        ],
        streamingText: "Swift concurrency is a powerful feature..."
    )
    .frame(width: 500, height: 400)
}
