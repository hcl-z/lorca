package app.lorca.core

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

/// The message body view: Markdown in, a selectable native text view out. The core parses;
/// the view renders and sizes itself to its text within `maxWidth`.
class MarkdownViewModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("MarkdownView")

    // The size a message takes within `maxWidth`. Synchronous, so the size lands in the same
    // commit as the text.
    Function("measure") { markdown: String, maxWidth: Double, fontSize: Double, codeFontSize: Double, sourceChips: Boolean ->
      val context = appContext.currentActivity ?: appContext.reactContext ?: throw IllegalStateException("No context to measure in")
      val style = MarkdownStyle().apply {
        this.markdown = markdown
        this.maxWidth = maxWidth.toFloat()
        this.fontSize = fontSize.toFloat()
        this.codeFontSize = codeFontSize.toFloat()
        this.sourceChips = sourceChips
      }
      val (width, height) = measureMessage(context, style)
      mapOf("width" to width, "height" to height)
    }

    // The sites a message's source chips name, whose icons JS asks the CLI for.
    Function("sourceHosts") { markdown: String -> uniffi.lorca_markdown.sourceHosts(markdown) }

    // A site's icon as base64 image bytes, or null when it has none; every chip of it redraws.
    Function("setFavicon") { host: String, data: String? ->
      SourceIcons.set(host, data?.let { android.util.Base64.decode(it, android.util.Base64.DEFAULT) })
    }

    View(MarkdownView::class) {
      Prop("markdown") { view: MarkdownView, markdown: String -> view.style.markdown = markdown }
      Prop("maxWidth") { view: MarkdownView, width: Double -> view.style.maxWidth = width.toFloat() }
      Prop("fontSize") { view: MarkdownView, size: Double -> view.style.fontSize = size.toFloat() }
      Prop("codeFontSize") { view: MarkdownView, size: Double -> view.style.codeFontSize = size.toFloat() }
      Prop("color") { view: MarkdownView, color: Int -> view.style.color = color }
      Prop("linkColor") { view: MarkdownView, color: Int -> view.style.linkColor = color }
      Prop("codeBackground") { view: MarkdownView, color: Int -> view.style.codeBackground = color }
      Prop("quoteColor") { view: MarkdownView, color: Int -> view.style.quoteColor = color }
      Prop("border") { view: MarkdownView, color: Int -> view.style.border = color }
      Prop("tint") { view: MarkdownView, color: Int -> view.style.tint = color }
      Prop("sourceChips") { view: MarkdownView, chips: Boolean -> view.style.sourceChips = chips }
      Prop("chipColor") { view: MarkdownView, color: Int -> view.style.chipColor = color }
      Prop("chipBorder") { view: MarkdownView, color: Int -> view.style.chipBorder = color }
      OnViewDidUpdateProps { view: MarkdownView -> view.render() }
    }
  }
}
