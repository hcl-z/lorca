import ExpoModulesCore

/// The message body view: Markdown in, a selectable native text view out. The core parses;
/// the view renders and sizes itself to its text within `maxWidth`.
public class MarkdownViewModule: Module {
  public func definition() -> ModuleDefinition {
    Name("MarkdownView")

    // The size a message takes within `maxWidth`. Synchronous, so the size lands in the same
    // commit as the text.
    Function("measure") { (markdown: String, maxWidth: Double, fontSize: Double, codeFontSize: Double, sourceChips: Bool) -> [String: Double] in
      var style = MarkdownStyle()
      style.sourceChips = sourceChips
      style.markdown = markdown
      style.maxWidth = CGFloat(maxWidth)
      style.fontSize = CGFloat(fontSize)
      style.codeFontSize = CGFloat(codeFontSize)
      let size = MarkdownMeasure.message(style)
      return ["width": Double(size.width), "height": Double(size.height)]
    }

    // The sites a message's source chips name, whose icons JS asks the CLI for.
    Function("sourceHosts") { (markdown: String) -> [String] in
      sourceHosts(markdown: markdown)
    }

    // A site's icon as base64 image bytes, or nil when it has none; every chip of it redraws.
    Function("setFavicon") { (host: String, data: String?) in
      SourceIcons.set(host, data.flatMap { Data(base64Encoded: $0) })
    }

    View(MarkdownView.self) {
      Prop("markdown") { (view: MarkdownView, markdown: String) in
        view.style.markdown = markdown
      }
      Prop("maxWidth") { (view: MarkdownView, width: Double) in
        view.style.maxWidth = CGFloat(width)
      }
      Prop("fontSize") { (view: MarkdownView, size: Double) in
        view.style.fontSize = CGFloat(size)
      }
      Prop("codeFontSize") { (view: MarkdownView, size: Double) in
        view.style.codeFontSize = CGFloat(size)
      }
      Prop("color") { (view: MarkdownView, color: UIColor) in
        view.style.color = color
      }
      Prop("linkColor") { (view: MarkdownView, color: UIColor) in
        view.style.linkColor = color
      }
      Prop("codeBackground") { (view: MarkdownView, color: UIColor) in
        view.style.codeBackground = color
      }
      Prop("quoteColor") { (view: MarkdownView, color: UIColor) in
        view.style.quoteColor = color
      }
      Prop("border") { (view: MarkdownView, color: UIColor) in
        view.style.border = color
      }
      Prop("tint") { (view: MarkdownView, color: UIColor) in
        view.style.tint = color
      }
      Prop("sourceChips") { (view: MarkdownView, chips: Bool) in
        view.style.sourceChips = chips
      }
      Prop("chipColor") { (view: MarkdownView, color: UIColor) in
        view.style.chipColor = color
      }
      Prop("chipBorder") { (view: MarkdownView, color: UIColor) in
        view.style.chipBorder = color
      }
      OnViewDidUpdateProps { (view: MarkdownView) in
        view.render()
      }
    }
  }
}
