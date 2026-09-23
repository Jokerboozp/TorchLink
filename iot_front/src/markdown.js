const htmlEntities = { /* 声明 htmlEntities。 */
  '&': '&amp;', /* 执行当前语句并推进处理流程。 */
  '<': '&lt;', /* 执行当前语句并推进处理流程。 */
  '>': '&gt;', /* 执行当前语句并推进处理流程。 */
  '"': '&quot;', /* 执行当前语句并推进处理流程。 */
  "'": '&#39;' /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function escapeHtml(value) { /* 定义 escapeHtml 函数。 */
  return String(value ?? '').replace(/[&<>"']/g, character => htmlEntities[character]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function safeHref(value) { /* 定义 safeHref 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    const url = new URL(value) /* 声明 url。 */
    return ['http:', 'https:', 'mailto:'].includes(url.protocol) ? value : '' /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    return '' /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function restoreTokens(value, tokens) { /* 定义 restoreTokens 函数。 */
  return value.replace(/\u0000(\d+)\u0000/g, (_, index) => tokens[Number(index)] || '') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function renderInline(value) { /* 定义 renderInline 函数。 */
  const tokens = [] /* 声明 tokens。 */
  const token = html => { /* 声明 token。 */
    const index = tokens.push(html) - 1 /* 声明 index。 */
    return `\u0000${index}\u0000` /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  let text = escapeHtml(value) /* 声明 text。 */

  text = text.replace(/`([^`\n]+)`/g, (_, code) => token(`<code>${code}</code>`))
  text = text.replace(/!\[([^\]\n]*)\]\([^\)\n]+\)/g, '$1')
  text = text.replace(/\[([^\]\n]+)\]\(((?:https?:\/\/|mailto:)[^\s\)]+)\)/g, (_, label, href) => {
    const safe = safeHref(href)
    return safe ? token(`<a href="${safe}" target="_blank" rel="noopener noreferrer">${label}</a>`) : label
  })
  text = text.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
  text = text.replace(/__([^_\n]+)__/g, '<strong>$1</strong>')
  text = text.replace(/~~([^~\n]+)~~/g, '<del>$1</del>')
  text = text.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>')

  return restoreTokens(text, tokens)
}

function splitTableRow(line) {
  let value = line.trim()
  if (value.startsWith('|')) value = value.slice(1)
  if (value.endsWith('|') && !value.endsWith('\\|')) value = value.slice(0, -1)
  return value.split('|').map(cell => cell.trim())
}

function isTableSeparator(line) {
  const cells = splitTableRow(line)
  return cells.length >= 2 && cells.every(cell => /^:?-{3,}:?$/.test(cell))
}

function renderTable(lines) {
  const headers = splitTableRow(lines[0])
  const rows = lines.slice(2).map(splitTableRow)
  const headerHtml = headers.map(cell => `<th>${renderInline(cell)}</th>`).join('')
  const rowHtml = rows.map(row => {
    const cells = headers.map((_, index) => `<td>${renderInline(row[index] || '')}</td>`).join('')
    return `<tr>${cells}</tr>`
  }).join('')
  return `<div class="markdown-table-wrap"><table><thead><tr>${headerHtml}</tr></thead><tbody>${rowHtml}</tbody></table></div>`
}

function isBlockStart(lines, index) {
  const line = lines[index] || ''
  if (!line.trim()) return true
  if (/^ {0,3}(?:`{3,}|~{3,})/.test(line)) return true
  if (/^ {0,3}#{1,6}\s+/.test(line)) return true /* 判断条件并选择处理分支。 */
  if (/^ {0,3}>\s?/.test(line)) return true /* 判断条件并选择处理分支。 */
  if (/^ {0,3}(?:[-+*]|\d+[.])\s+/.test(line)) return true /* 判断条件并选择处理分支。 */
  if (/^\s*(?:\*{3,}|-{3,}|_{3,})\s*$/.test(line)) return true /* 判断条件并选择处理分支。 */
  return Boolean(lines[index + 1] && isTableSeparator(lines[index + 1]) && line.includes('|')) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function renderFence(lines, start) { /* 定义 renderFence 函数。 */
  const opening = lines[start].match(/^ {0,3}(`{3,}|~{3,})\s*([^\s]*)?.*$/)
  const marker = opening?.[1]?.[0] || '`'
  const markerLength = opening?.[1]?.length || 3 /* 声明 markerLength。 */
  const language = (opening?.[2] || '').match(/^[A-Za-z0-9_-]+/)?.[0] || '' /* 声明 language。 */
  const closingPattern = new RegExp(`^ {0,3}${marker}{${markerLength},}\\s*$`) /* 声明 closingPattern。 */
  const content = [] /* 声明 content。 */
  let index = start + 1 /* 声明 index。 */
  while (index < lines.length && !closingPattern.test(lines[index])) { /* 循环处理当前数据。 */
    content.push(lines[index]) /* 执行当前语句并推进处理流程。 */
    index += 1 /* 更新 index 的值。 */
  } /* 结束当前表达式或代码块。 */
  if (index < lines.length) index += 1 /* 判断条件并选择处理分支。 */
  const className = language ? ` class="language-${language}"` : '' /* 声明 className。 */
  return { html:`<pre><code${className}>${escapeHtml(content.join('\n'))}</code></pre>`, next:index } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function renderList(lines, start, ordered) { /* 定义 renderList 函数。 */
  const itemPattern = ordered ? /^ {0,3}\d+[.]\s+(.+)$/ : /^ {0,3}[-+*]\s+(.+)$/ /* 声明 itemPattern。 */
  const items = [] /* 声明 items。 */
  let index = start /* 声明 index。 */
  while (index < lines.length) { /* 循环处理当前数据。 */
    const match = lines[index].match(itemPattern) /* 声明 match。 */
    if (match) { /* 判断条件并选择处理分支。 */
      const content = [match[1]] /* 声明 content。 */
      index += 1 /* 更新 index 的值。 */
      while (index < lines.length && /^ {2,}\S/.test(lines[index]) && !itemPattern.test(lines[index])) { /* 循环处理当前数据。 */
        content.push(lines[index].trim()) /* 执行当前语句并推进处理流程。 */
        index += 1 /* 更新 index 的值。 */
      } /* 结束当前表达式或代码块。 */
      items.push(`<li>${renderInline(content.join('\n')).replace(/\n/g, '<br>')}</li>`) /* 执行当前语句并推进处理流程。 */
      continue /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    break /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return { html:`<${ordered ? 'ol' : 'ul'}>${items.join('')}</${ordered ? 'ol' : 'ul'}>`, next:index } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function renderMarkdown(source) { /* 执行当前语句并推进处理流程。 */
  const lines = String(source ?? '').replace(/\r\n?/g, '\n').split('\n') /* 声明 lines。 */
  const html = [] /* 声明 html。 */
  let index = 0 /* 声明 index。 */
  let paragraph = [] /* 声明 paragraph。 */

  const flushParagraph = () => { /* 声明 flushParagraph。 */
    if (!paragraph.length) return /* 判断条件并选择处理分支。 */
    html.push(`<p>${renderInline(paragraph.join('\n')).replace(/\n/g, '<br>')}</p>`) /* 执行当前语句并推进处理流程。 */
    paragraph = [] /* 更新 paragraph 的值。 */
  } /* 结束当前表达式或代码块。 */

  while (index < lines.length) { /* 循环处理当前数据。 */
    const line = lines[index] /* 声明 line。 */
    if (!line.trim()) { /* 判断条件并选择处理分支。 */
      flushParagraph() /* 执行当前语句并推进处理流程。 */
      index += 1 /* 更新 index 的值。 */
      continue /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */

    if (/^ {0,3}(?:`{3,}|~{3,})/.test(line)) {
      flushParagraph()
      const fence = renderFence(lines, index)
      html.push(fence.html)
      index = fence.next
      continue
    }

    const heading = line.match(/^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$/)
    if (heading) {
      flushParagraph()
      const level = heading[1].length
      html.push(`<h${level}>${renderInline(heading[2])}</h${level}>`)
      index += 1
      continue
    }

    if (lines[index + 1] && /^(?:\s*=+\s*|\s*-{3,}\s*)$/.test(lines[index + 1]) && line.trim()) {
      flushParagraph()
      const level = lines[index + 1].includes('=') ? 1 : 2
      html.push(`<h${level}>${renderInline(line.trim())}</h${level}>`)
      index += 2
      continue
    }

    if (/^\s*(?:\*{3,}|-{3,}|_{3,})\s*$/.test(line)) {
      flushParagraph()
      html.push('<hr>')
      index += 1
      continue
    }

    if (/^ {0,3}>\s?/.test(line)) {
      flushParagraph()
      const quoteLines = []
      while (index < lines.length && /^ {0,3}>\s?/.test(lines[index])) {
        quoteLines.push(lines[index].replace(/^ {0,3}>\s?/, ''))
        index += 1
      }
      html.push(`<blockquote>${renderMarkdown(quoteLines.join('\n'))}</blockquote>`)
      continue
    }

    const unordered = /^ {0,3}[-+*]\s+/.test(line)
    const ordered = /^ {0,3}\d+[.]\s+/.test(line)
    if (unordered || ordered) {
      flushParagraph()
      const list = renderList(lines, index, ordered)
      html.push(list.html)
      index = list.next
      continue
    }

    if (line.includes('|') && lines[index + 1] && isTableSeparator(lines[index + 1])) {
      flushParagraph()
      const tableLines = [line, lines[index + 1]]
      index += 2
      while (index < lines.length && lines[index].includes('|') && lines[index].trim()) {
        tableLines.push(lines[index])
        index += 1
      }
      html.push(renderTable(tableLines))
      continue
    }

    if (!paragraph.length || !isBlockStart(lines, index)) paragraph.push(line)
    else flushParagraph()
    index += 1
  }
  flushParagraph()
  return html.join('')
}
