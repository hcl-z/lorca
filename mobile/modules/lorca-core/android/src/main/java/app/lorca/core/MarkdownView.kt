package app.lorca.core

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.graphics.Typeface
import android.os.Build
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.method.LinkMovementMethod
import android.text.style.AbsoluteSizeSpan
import android.text.style.BackgroundColorSpan
import android.text.style.ForegroundColorSpan
import android.text.style.LeadingMarginSpan
import android.text.style.LineBackgroundSpan
import android.text.style.LineHeightSpan
import android.text.style.QuoteSpan
import android.text.style.StrikethroughSpan
import android.text.style.StyleSpan
import android.text.style.TabStopSpan
import android.text.style.TypefaceSpan
import android.text.style.URLSpan
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.HorizontalScrollView
import android.widget.LinearLayout
import android.widget.TextView
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.views.ExpoView
import uniffi.lorca_markdown.Align
import uniffi.lorca_markdown.Block
import uniffi.lorca_markdown.Cell
import uniffi.lorca_markdown.Document
import uniffi.lorca_markdown.Span
import uniffi.lorca_markdown.TableRow
import uniffi.lorca_markdown.parseMarkdown
import kotlin.math.ceil
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/// What the JS side sets: the Markdown, the width the bubble allows, and the message palette.
class MarkdownStyle {
  var markdown = ""
  var maxWidth = 0f
  var fontSize = 16.5f
  var codeFontSize = 14f
  var color = Color.BLACK
  var linkColor = Color.BLUE
  var codeBackground = 0x0F000000
  var quoteColor = 0x4D000000
  var border = 0x33000000
  var tint = Color.BLUE
  /// Web links as source chips: a bot's message, not the user's own bubble.
  var sourceChips = false
  var chipColor = Color.WHITE
  var chipBorder = 0x17000000
}

private object Layout {
  const val GAP = 8f
  const val ITEM_GAP = 3f
  const val ROW_GAP = 2f
  const val CODE_PADDING = 8f
  const val CODE_INSET = 10f
  const val CODE_CORNER = 8f
  const val LIST_INDENT = 22f
  const val QUOTE_INDENT = 10f
  const val QUOTE_BAR = 2f
  const val CELL_PADDING_X = 10f
  const val CELL_PADDING_Y = 7f
  const val COLUMN_FLOOR = 72f
  const val CHIP_ICON = 14f
  /// From the chip's start to its icon.
  const val CHIP_PAD = 6f
}

/// The sites' icons the app has found, by host, shared by every message. JS hands them over as
/// they arrive (`setFavicon`); the text views showing chips redraw then.
object SourceIcons {
  private val icons = java.util.concurrent.ConcurrentHashMap<String, android.graphics.Bitmap>()
  private val views = java.util.Collections.newSetFromMap(java.util.WeakHashMap<View, Boolean>())

  fun image(host: String): android.graphics.Bitmap? = icons[host]

  fun set(host: String, bytes: ByteArray?) {
    // BitmapFactory reads PNG, JPEG, GIF, WebP, and ICO.
    val bitmap = bytes?.let { android.graphics.BitmapFactory.decodeByteArray(it, 0, it.size) } ?: return
    icons[host] = bitmap
    android.os.Handler(android.os.Looper.getMainLooper()).post { synchronized(views) { views.toList() }.forEach { it.invalidate() } }
  }

  fun watch(view: View) {
    synchronized(views) { views.add(view) }
  }
}

/// A chip's text: a no-break space and room for the icon (two figure spaces) ahead of the label,
/// and the same behind it. The label's own spaces do not break, so the icon never parts from it.
/// The desktop app's `chipWords`, word joiner and all, so every app's chip reads the same.
private fun chipWords(label: String) = "\u00a0\u00a0\u2007\u2007\u00a0" + label.replace(' ', '\u00a0') + "\u00a0\u00a0\u2060"

/// A source chip drawn in place of its words: a rounded fill with a hairline, the site's icon (a
/// globe until it has one) in the room at its start, and the words. As wide as the words are at
/// the chip's size, so measuring and drawing agree; the URLSpan over it still opens the link.
private class SourceChipSpan(
  private val host: String,
  private val textSize: Float,
  private val bold: Boolean,
  private val fill: Int,
  private val border: Int,
  private val color: Int,
  private val density: Float,
) : android.text.style.ReplacementSpan() {
  private val shape = Paint(Paint.ANTI_ALIAS_FLAG)

  private fun styled(paint: Paint): Paint = Paint(paint).apply {
    textSize = this@SourceChipSpan.textSize
    typeface = Typeface.create(Typeface.DEFAULT, if (bold) Typeface.BOLD else Typeface.NORMAL)
    isFakeBoldText = false
    color = this@SourceChipSpan.color
  }

  override fun getSize(paint: Paint, text: CharSequence, start: Int, end: Int, fm: Paint.FontMetricsInt?): Int {
    val words = styled(paint)
    if (fm != null) paint.getFontMetricsInt(fm)
    return ceil(words.measureText(text, start, end)).toInt()
  }

  override fun draw(canvas: Canvas, text: CharSequence, start: Int, end: Int, x: Float, top: Int, y: Int, bottom: Int, paint: Paint) {
    val words = styled(paint)
    val width = words.measureText(text, start, end)
    val metrics = words.fontMetrics
    val height = (metrics.descent - metrics.ascent) + 3 * density
    // Centered on the words' own middle, a little under the line's.
    val middle = y + (metrics.ascent + metrics.descent) / 2 + 0.5f * density
    val rect = RectF(x, middle - height / 2, x + width, middle + height / 2)
    shape.style = Paint.Style.FILL
    shape.color = fill
    canvas.drawRoundRect(rect, height / 2, height / 2, shape)
    shape.style = Paint.Style.STROKE
    shape.strokeWidth = 1f
    shape.color = border
    canvas.drawRoundRect(rect, height / 2, height / 2, shape)
    val size = (Layout.CHIP_ICON * density).roundToInt()
    val left = (x + Layout.CHIP_PAD * density).roundToInt()
    val iconTop = (middle - size / 2f).roundToInt()
    val icon = SourceIcons.image(host)
    if (icon != null) {
      canvas.drawBitmap(icon, null, android.graphics.Rect(left, iconTop, left + size, iconTop + size), Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG))
    } else {
      drawGlobe(canvas, RectF(left.toFloat(), iconTop.toFloat(), (left + size).toFloat(), (iconTop + size).toFloat()))
    }
    canvas.drawText(text, start, end, x, y.toFloat(), words)
  }

  /// A globe in outline, the words' color at 55%: the circle, a meridian, and the equator.
  private fun drawGlobe(canvas: Canvas, box: RectF) {
    val inset = 1.2f * density
    val r = RectF(box.left + inset, box.top + inset, box.right - inset, box.bottom - inset)
    shape.style = Paint.Style.STROKE
    shape.strokeWidth = 1.2f * density
    shape.color = (color and 0x00FFFFFF) or 0x8C000000.toInt()
    canvas.drawOval(r, shape)
    val meridian = RectF(r.centerX() - r.width() / 4, r.top, r.centerX() + r.width() / 4, r.bottom)
    canvas.drawOval(meridian, shape)
    canvas.drawLine(r.left, r.centerY(), r.right, r.centerY(), shape)
  }
}

/// Fixes a paragraph's line height and adds the space above it and, for a code block, below it.
private class ParagraphSpan(private val lineHeight: Int, private val before: Int, private val after: Int) : LineHeightSpan {
  override fun chooseHeight(text: CharSequence, start: Int, end: Int, spanstartv: Int, v: Int, fm: Paint.FontMetricsInt) {
    val origin = fm.descent - fm.ascent
    if (origin > 0) {
      val ratio = lineHeight.toFloat() / origin
      fm.descent = (fm.descent * ratio).roundToInt()
      fm.ascent = (fm.ascent * ratio).roundToInt()
      fm.top = fm.ascent
      fm.bottom = fm.descent
    }
    val spanned = text as? Spanned ?: return
    if (start <= spanned.getSpanStart(this)) {
      fm.ascent -= before
      fm.top -= before
    }
    if (end >= spanned.getSpanEnd(this)) {
      fm.descent += after
      fm.bottom += after
    }
  }
}

/// The box behind a fenced code block: one rounded rectangle across its lines.
private class CodeBlockSpan(private val color: Int, private val left: Float, private val corner: Float) : LineBackgroundSpan {
  private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.FILL }
  private val path = Path()

  override fun drawBackground(canvas: Canvas, p: Paint, left: Int, right: Int, top: Int, baseline: Int, bottom: Int, text: CharSequence, start: Int, end: Int, lnum: Int) {
    val spanned = text as? Spanned ?: return
    val first = start <= spanned.getSpanStart(this)
    val last = end >= spanned.getSpanEnd(this)
    val rect = RectF(left + this.left, top.toFloat(), right.toFloat(), bottom.toFloat())
    val r = corner
    val radii = floatArrayOf(
      if (first) r else 0f, if (first) r else 0f, if (first) r else 0f, if (first) r else 0f,
      if (last) r else 0f, if (last) r else 0f, if (last) r else 0f, if (last) r else 0f,
    )
    path.reset()
    path.addRoundRect(rect, radii, Path.Direction.CW)
    paint.color = color
    canvas.drawPath(path, paint)
  }
}

/// A message is text runs and, between them, the tables that get a grid of their own.
private sealed class Segment {
  class Text(val text: CharSequence) : Segment()
  class Table(val alignments: List<Align>, val header: List<Cell>, val rows: List<TableRow>) : Segment()
}

/// Turns the core's document into Spannables: spans per block for margins, line heights, and
/// backgrounds; spans per run for the inline styles. Top-level tables become segments; a
/// table inside a list or quote is written as lines of cells.
private class MarkdownRenderer(context: Context, private val style: MarkdownStyle) {
  private var out = SpannableStringBuilder()
  /// Whether a chip was written, so the views showing it redraw when an icon arrives.
  var hasChips = false
  private val density = context.resources.displayMetrics.density
  private val scaledDensity = context.resources.displayMetrics.scaledDensity
  /// The space the previous block asked for below itself; the next paragraph takes it above.
  private var pending = 0f

  private data class Ctx(val indent: Float = 0f, val quote: Boolean = false, val marker: String? = null, val markerX: Float = 0f)

  fun dp(v: Float) = (v * density).roundToInt()
  fun sp(v: Float) = (v * scaledDensity).roundToInt()

  fun render(document: Document): List<Segment> {
    val segments = mutableListOf<Segment>()
    val blocks = document.blocks
    blocks.forEachIndexed { index, block ->
      if (block is Block.Table) {
        flush(segments)
        segments.add(Segment.Table(block.alignments, block.header, block.rows))
        return@forEachIndexed
      }
      val next = blocks.getOrNull(index + 1)
      val breaks = next == null || next is Block.Table
      block(block, Ctx(), if (breaks) 0f else Layout.GAP)
    }
    flush(segments)
    return segments
  }

  private fun flush(segments: MutableList<Segment>) {
    if (out.isEmpty()) return
    segments.add(Segment.Text(out))
    out = SpannableStringBuilder()
    pending = 0f
  }

  /// One table cell, for the grid.
  fun cell(spans: List<Span>, header: Boolean): CharSequence {
    val text = SpannableStringBuilder()
    val size = style.fontSize - 1
    for (span in chipSpans(spans)) {
      if (chip(text, span, size)) continue
      val from = text.length
      text.append(span.text)
      val to = text.length
      if (from == to) continue
      inline(text, span, from, to, size, header)
    }
    if (text.isEmpty()) text.append(" ")
    text.setSpan(AbsoluteSizeSpan(sp(size)), 0, text.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    if (header) text.setSpan(StyleSpan(Typeface.BOLD), 0, text.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    return text
  }

  private fun blocks(blocks: List<Block>, ctx: Ctx, after: Float) {
    blocks.forEachIndexed { index, block ->
      val inner = if (index > 0) ctx.copy(marker = null) else ctx
      block(block, inner, if (index == blocks.lastIndex) after else Layout.GAP)
    }
  }

  private fun block(block: Block, ctx: Ctx, after: Float) {
    when (block) {
      is Block.Paragraph -> paragraph(block.spans, ctx, style.fontSize, style.fontSize * 1.35f, after)
      is Block.Heading -> {
        val size = if (block.level.toInt() <= 2) style.fontSize + 2 else style.fontSize
        paragraph(block.spans, ctx, size, (style.fontSize + 2) * 1.3f, after, bold = true)
      }
      is Block.Code -> code(block.text, ctx, after)
      is Block.Listing -> block.items.forEachIndexed { index, item ->
        val inner = ctx.copy(
          marker = marker(block.ordered, block.start.toInt() + index, item.checked),
          markerX = ctx.indent,
          indent = ctx.indent + Layout.LIST_INDENT,
        )
        val itemAfter = if (index == block.items.lastIndex) after else Layout.ITEM_GAP
        if (item.blocks.isEmpty()) paragraph(emptyList(), inner, style.fontSize, style.fontSize * 1.35f, itemAfter)
        else blocks(item.blocks, inner, itemAfter)
      }
      is Block.Quote -> blocks(block.blocks, ctx.copy(quote = true, indent = ctx.indent + Layout.QUOTE_INDENT + Layout.QUOTE_BAR), after)
      is Block.Table -> {
        paragraph(cells(block.header), ctx, style.fontSize, style.fontSize * 1.35f, if (block.rows.isEmpty()) after else Layout.ROW_GAP, bold = true)
        block.rows.forEachIndexed { index, row ->
          paragraph(cells(row.cells), ctx, style.fontSize, style.fontSize * 1.35f, if (index == block.rows.lastIndex) after else Layout.ROW_GAP)
        }
      }
      is Block.Rule -> paragraph(listOf(Span("─".repeat(24), false, false, false, false, null)), ctx, style.fontSize, style.fontSize * 1.35f, after, color = style.quoteColor)
    }
  }

  /// One row of a nested table: its cells on a line, three spaces apart.
  private fun cells(cells: List<Cell>): List<Span> {
    val spans = mutableListOf<Span>()
    cells.forEachIndexed { index, cell ->
      if (index > 0) spans.add(Span("   ", false, false, false, false, null))
      spans.addAll(cell.spans)
    }
    return spans
  }

  private fun marker(ordered: Boolean, number: Int, checked: Boolean?): String {
    if (checked != null) return if (checked) "☑\t" else "☐\t"
    return if (ordered) "$number.\t" else "•\t"
  }

  /// Ends the previous paragraph and returns where the new one starts.
  private fun open(): Int {
    if (out.isNotEmpty()) out.append("\n")
    return out.length
  }

  /// The runs of one web link as one span, so the link is one chip.
  private fun chipSpans(spans: List<Span>): List<Span> = if (style.sourceChips) uniffi.lorca_markdown.joinLinkSpans(spans) else spans

  /// Writes a web link as a source chip; false for a link that stays link text, or any other run.
  private fun chip(text: SpannableStringBuilder, span: Span, fontSize: Float): Boolean {
    if (!style.sourceChips || span.code) return false
    val link = span.link ?: return false
    val chip = uniffi.lorca_markdown.sourceChip(span.text, link) ?: return false
    val from = text.length
    text.append(chipWords(chip.label))
    val to = text.length
    text.setSpan(SourceChipSpan(chip.host, sp(fontSize - 1.5f).toFloat(), span.bold, style.chipColor, style.chipBorder, style.color, density), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    text.setSpan(URLSpan(link), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    hasChips = true
    return true
  }

  /// The run's own styles. A bold paragraph carries its own bold span; a run only adds what
  /// the paragraph lacks.
  private fun inline(text: SpannableStringBuilder, span: Span, from: Int, to: Int, fontSize: Float, bold: Boolean) {
    if (span.code) {
      text.setSpan(TypefaceSpan("monospace"), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
      text.setSpan(AbsoluteSizeSpan(sp(fontSize - 1.5f)), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
      text.setSpan(BackgroundColorSpan(style.codeBackground), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    }
    val weight = when {
      !bold && span.bold && span.italic -> Typeface.BOLD_ITALIC
      !bold && span.bold -> Typeface.BOLD
      span.italic -> Typeface.ITALIC
      else -> null
    }
    if (weight != null) text.setSpan(StyleSpan(weight), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    if (span.strike) text.setSpan(StrikethroughSpan(), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    span.link?.let { text.setSpan(URLSpan(it), from, to, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE) }
  }

  private fun paragraph(spans: List<Span>, ctx: Ctx, fontSize: Float, lineHeight: Float, after: Float, bold: Boolean = false, color: Int? = null) {
    val start = open()
    ctx.marker?.let { out.append(it) }
    for (span in chipSpans(spans)) {
      if (chip(out, span, fontSize)) continue
      val from = out.length
      out.append(span.text)
      val to = out.length
      if (from == to) continue
      inline(out, span, from, to, fontSize, bold)
    }
    if (out.length == start) out.append(" ")
    val end = out.length
    out.setSpan(AbsoluteSizeSpan(sp(fontSize)), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    if (bold) out.setSpan(StyleSpan(Typeface.BOLD), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    color?.let { out.setSpan(ForegroundColorSpan(it), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE) }
    layout(start, end, ctx, lineHeight, after = 0f)
    pending = after
  }

  private fun code(text: String, ctx: Ctx, after: Float) {
    val start = open()
    ctx.marker?.let { out.append(it) }
    out.append(text.ifEmpty { " " })
    val end = out.length
    out.setSpan(TypefaceSpan("monospace"), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    out.setSpan(AbsoluteSizeSpan(sp(style.codeFontSize)), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    out.setSpan(CodeBlockSpan(style.codeBackground, dp(ctx.indent).toFloat(), dp(Layout.CODE_CORNER).toFloat()), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    // The box is painted over the line box, so the padding is extra line height above and below.
    layout(start, end, ctx.copy(indent = ctx.indent + Layout.CODE_INSET), style.codeFontSize * 1.4f, before = Layout.CODE_PADDING, after = Layout.CODE_PADDING)
    pending = after
  }

  private fun layout(start: Int, end: Int, ctx: Ctx, lineHeight: Float, before: Float = 0f, after: Float) {
    val space = pending + before
    out.setSpan(ParagraphSpan(sp(lineHeight), dp(space), dp(after)), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    if (ctx.quote) {
      val quote = if (Build.VERSION.SDK_INT >= 28) QuoteSpan(style.quoteColor, dp(Layout.QUOTE_BAR), dp(Layout.QUOTE_INDENT)) else QuoteSpan(style.quoteColor)
      out.setSpan(quote, start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    }
    val base = ctx.indent - (if (ctx.quote) Layout.QUOTE_INDENT + Layout.QUOTE_BAR else 0f)
    if (ctx.marker != null) {
      out.setSpan(LeadingMarginSpan.Standard(dp(ctx.markerX), dp(base)), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
      out.setSpan(TabStopSpan.Standard(dp(ctx.indent)), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    } else if (base > 0f) {
      out.setSpan(LeadingMarginSpan.Standard(dp(base)), start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    }
    pending = 0f
  }
}

/// A TextView styled for message text: selectable with tappable links, or, to measure, plain.
private fun messageText(context: Context, style: MarkdownStyle, text: CharSequence, interactive: Boolean = true): TextView =
  TextView(context).apply {
    if (interactive) setTextIsSelectable(true)
    background = null
    includeFontPadding = false
    setPadding(0, 0, 0, 0)
    setTextColor(style.color)
    setLinkTextColor(style.linkColor)
    highlightColor = (style.tint and 0x00FFFFFF) or 0x40000000
    setTextSize(TypedValue.COMPLEX_UNIT_SP, style.fontSize)
    this.text = text
    if (interactive) movementMethod = LinkMovementMethod.getInstance()
  }

/// A table as a grid: columns measured from their cells and shrunk to fit when they must,
/// rows as tall as their tallest cell, a filled header row, hairline rules, rounded corners.
/// Its parent scrolls it sideways when it is wider than the bubble.
private class MarkdownTableView(
  context: Context,
  private val style: MarkdownStyle,
  spec: Segment.Table,
  renderer: MarkdownRenderer,
  /// The width the bubble allows, in px.
  private val maxWidth: Int,
  interactive: Boolean,
) : ViewGroup(context) {
  private val density = context.resources.displayMetrics.density
  private val cells: List<List<TextView>>
  private val columns: Int
  private var widths = IntArray(0)
  private var heights = IntArray(0)
  private val paint = Paint(Paint.ANTI_ALIAS_FLAG)

  init {
    setWillNotDraw(false)
    val rows = listOf(spec.header) + spec.rows.map { it.cells }
    columns = rows.maxOfOrNull { it.size } ?: 0
    val padX = renderer.dp(Layout.CELL_PADDING_X)
    val padY = renderer.dp(Layout.CELL_PADDING_Y)
    cells = rows.mapIndexed { r, row ->
      (0 until columns).map { c ->
        val spans = row.getOrNull(c)?.spans ?: emptyList()
        messageText(context, style, renderer.cell(spans, header = r == 0), interactive).apply {
          setPadding(padX, padY, padX, padY)
          gravity = when (spec.alignments.getOrNull(c)) {
            Align.CENTER -> Gravity.CENTER_HORIZONTAL
            Align.RIGHT -> Gravity.END
            else -> Gravity.START
          }
          addView(this)
        }
      }
    }
  }

  override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
    if (columns == 0) {
      setMeasuredDimension(0, 0)
      return
    }
    val unspecified = MeasureSpec.makeMeasureSpec(0, MeasureSpec.UNSPECIFIED)
    val natural = FloatArray(columns)
    for (row in cells) row.forEachIndexed { c, cell ->
      cell.measure(unspecified, unspecified)
      natural[c] = max(natural[c], cell.measuredWidth.toFloat())
    }
    val total = natural.sum()
    val fitted = natural.copyOf()
    if (total > maxWidth) {
      // Take the excess from the wide columns first; no column drops below the floor.
      val floor = Layout.COLUMN_FLOOR * density
      val flexible = FloatArray(columns) { max(0f, natural[it] - floor) }
      val give = flexible.sum()
      val excess = min(total - maxWidth, give)
      if (give > 0f) for (c in 0 until columns) fitted[c] = natural[c] - flexible[c] / give * excess
    }
    widths = IntArray(columns) { ceil(fitted[it]).toInt() }
    heights = IntArray(cells.size)
    cells.forEachIndexed { r, row ->
      var height = 0
      row.forEachIndexed { c, cell ->
        cell.measure(MeasureSpec.makeMeasureSpec(widths[c], MeasureSpec.EXACTLY), unspecified)
        height = max(height, cell.measuredHeight)
      }
      heights[r] = height
    }
    setMeasuredDimension(widths.sum(), heights.sum())
  }

  override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
    var y = 0
    cells.forEachIndexed { row, views ->
      var x = 0
      views.forEachIndexed { c, cell ->
        cell.layout(x, y, x + widths[c], y + heights[row])
        x += widths[c]
      }
      y += heights[row]
    }
  }

  /// Rules between the rows: a stronger one under the header, hairlines below.
  override fun dispatchDraw(canvas: Canvas) {
    super.dispatchDraw(canvas)
    if (heights.size < 2) return
    val hairline = max(1f, density)
    paint.style = Paint.Style.FILL
    var y = 0f
    for (r in 0 until heights.size - 1) {
      y += heights[r]
      paint.color = if (r == 0) style.quoteColor else style.border
      canvas.drawRect(0f, y - hairline / 2, width.toFloat(), y + hairline / 2, paint)
    }
  }
}

/// Fills `parent` with a message's views: the text runs as TextViews and the tables as grids,
/// stacked with the block gap, then measures it against `maxWidth`. Returns the size in dp.
private fun layOutMessage(parent: LinearLayout, style: MarkdownStyle, interactive: Boolean): Pair<Double, Double> {
  val context = parent.context
  val density = context.resources.displayMetrics.density
  val maxPx = max(1, (style.maxWidth * density).roundToInt())
  val renderer = MarkdownRenderer(context, style)
  parent.removeAllViews()
  renderer.render(parseMarkdown(style.markdown)).forEachIndexed { index, segment ->
    val view: View = when (segment) {
      is Segment.Text -> messageText(context, style, segment.text, interactive).also { if (interactive && renderer.hasChips) SourceIcons.watch(it) }
      is Segment.Table -> HorizontalScrollView(context).apply {
        isHorizontalScrollBarEnabled = false
        overScrollMode = View.OVER_SCROLL_NEVER
        addView(MarkdownTableView(context, style, segment, renderer, maxPx, interactive), ViewGroup.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT))
      }
    }
    val params = LinearLayout.LayoutParams(LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT)
    if (index > 0) params.topMargin = renderer.dp(Layout.GAP)
    parent.addView(view, params)
  }
  parent.measure(
    View.MeasureSpec.makeMeasureSpec(maxPx, View.MeasureSpec.AT_MOST),
    View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED),
  )
  return Pair(parent.measuredWidth / density.toDouble(), parent.measuredHeight / density.toDouble())
}

/// The size a message takes within `maxWidth`, in dp, from the same views the message view
/// builds. Called on the JS thread, so the size lands in the same commit as the text; the views
/// are detached and plain, which lays the text out exactly as the selectable ones do.
fun measureMessage(context: Context, style: MarkdownStyle): Pair<Double, Double> =
  layOutMessage(LinearLayout(context).apply { orientation = LinearLayout.VERTICAL }, style, interactive = false)

/// A message body as native views: the core parses the Markdown, this renders the text runs
/// into selectable TextViews and the tables into grids, stacked with the block gap. JS sizes the
/// view from `measureMessage` before it mounts, so the rows of a transcript have their heights
/// in their first layout.
class MarkdownView(context: Context, appContext: AppContext) : ExpoView(context, appContext) {
  val style = MarkdownStyle()

  override val shouldUseAndroidLayout = true

  init {
    orientation = VERTICAL
  }

  fun render() {
    layOutMessage(this, style, interactive = true)
  }
}
