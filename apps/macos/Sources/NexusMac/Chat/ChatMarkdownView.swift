import SwiftUI

/// Renders markdown content in chat messages
struct ChatMarkdownView: View {
    let content: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(parseBlocks().enumerated()), id: \.offset) { _, block in
                renderBlock(block)
            }
        }
    }

    private func parseBlocks() -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var currentText = ""
        var inCodeBlock = false
        var codeLanguage = ""
        var codeContent = ""

        for line in content.components(separatedBy: "\n") {
            if line.hasPrefix("```") {
                if inCodeBlock {
                    // End code block
                    blocks.append(.code(language: codeLanguage, content: codeContent.trimmingCharacters(in: .newlines)))
                    codeContent = ""
                    codeLanguage = ""
                    inCodeBlock = false
                } else {
                    // Start code block - flush current text
                    if !currentText.isEmpty {
                        blocks.append(.text(currentText.trimmingCharacters(in: .newlines)))
                        currentText = ""
                    }
                    codeLanguage = String(line.dropFirst(3))
                    inCodeBlock = true
                }
            } else if inCodeBlock {
                codeContent += line + "\n"
            } else {
                currentText += line + "\n"
            }
        }

        // Flush remaining content
        if !currentText.isEmpty {
            blocks.append(.text(currentText.trimmingCharacters(in: .newlines)))
        }

        return blocks
    }

    @ViewBuilder
    private func renderBlock(_ block: MarkdownBlock) -> some View {
        switch block {
        case .text(let content):
            Text(LocalizedStringKey(content))
                .textSelection(.enabled)
        case .code(let language, let content):
            CodeBlockView(language: language, content: content)
        }
    }
}

enum MarkdownBlock {
    case text(String)
    case code(language: String, content: String)
}

/// Code block with syntax highlighting
struct CodeBlockView: View {
    let language: String
    let content: String

    @State private var isCopied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Text(language.isEmpty ? "code" : language)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    copyCode()
                } label: {
                    Image(systemName: isCopied ? "checkmark" : "doc.on.doc")
                        .font(.caption)
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(Color.secondary.opacity(0.1))

            // Code content
            ScrollView(.horizontal, showsIndicators: false) {
                Text(content)
                    .font(.system(.caption, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(12)
            }
        }
        .background(Color.secondary.opacity(0.05))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func copyCode() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(content, forType: .string)

        isCopied = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            isCopied = false
        }
    }
}

#Preview {
    ChatMarkdownView(content: """
    Here's some code:

    ```swift
    func greet() {
        print("Hello, World!")
    }
    ```

    And some more text with **bold** and *italic*.
    """)
    .padding()
    .frame(width: 400)
}
