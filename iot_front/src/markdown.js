const htmlEntities = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;'
}

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, character => htmlEntities[character])
}

function safeHref(value) {
  try {
    const url = new URL(value)
    return ['http:', 'https:', 'mailto:'].includes(url.protocol) ? value : ''
  } catch {
    return ''
  }
}

function restoreTokens(value, tokens) {
  return value.replace(/\u0000(\d+)\u0000/g, (_, index) => tokens[Number(index)] || '')
}

function renderInline(value) {
  const tokens = []
  const token = html => {
    const index = tokens.push(html) - 1
    return `\u0000${index}\u0000`
  }
  let text = escapeHtml(value)

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
  if (/^ {0,3}#{1,6}\s+/.test(line)) return true
  if (/^ {0,3}>\s?/.test(line)) return true
  if (/^ {0,3}(?:[-+*]|\d+[.])\s+/.test(line)) return true
  if (/^\s*(?:\*{3,}|-{3,}|_{3,})\s*$/.test(line)) return true
  return Boolean(lines[index + 1] && isTableSeparator(lines[index + 1]) && line.includes('|'))
}

function renderFence(lines, start) {
  const opening = lines[start].match(/^ {0,3}(`{3,}|~{3,})\s*([^\s]*)?.*$/)
  const marker = opening?.[1]?.[0] || '`'
  const markerLength = opening?.[1]?.length || 3
  const language = (opening?.[2] || '').match(/^[A-Za-z0-9_-]+/)?.[0] || ''
  const closingPattern = new RegExp(`^ {0,3}${marker}{${markerLength},}\\s*$`)
  const content = []
  let index = start + 1
  while (index < lines.length && !closingPattern.test(lines[index])) {
    content.push(lines[index])
    index += 1
  }
  if (index < lines.length) index += 1
  const className = language ? ` class="language-${language}"` : ''
  return { html:`<pre><code${className}>${escapeHtml(content.join('\n'))}</code></pre>`, next:index }
}

function renderList(lines, start, ordered) {
  const itemPattern = ordered ? /^ {0,3}\d+[.]\s+(.+)$/ : /^ {0,3}[-+*]\s+(.+)$/
  const items = []
  let index = start
  while (index < lines.length) {
    const match = lines[index].match(itemPattern)
    if (match) {
      const content = [match[1]]
      index += 1
      while (index < lines.length && /^ {2,}\S/.test(lines[index]) && !itemPattern.test(lines[index])) {
        content.push(lines[index].trim())
        index += 1
      }
      items.push(`<li>${renderInline(content.join('\n')).replace(/\n/g, '<br>')}</li>`)
      continue
    }
    break
  }
  return { html:`<${ordered ? 'ol' : 'ul'}>${items.join('')}</${ordered ? 'ol' : 'ul'}>`, next:index }
}

export function renderMarkdown(source) {
  const lines = String(source ?? '').replace(/\r\n?/g, '\n').split('\n')
  const html = []
  let index = 0
  let paragraph = []

  const flushParagraph = () => {
    if (!paragraph.length) return
    html.push(`<p>${renderInline(paragraph.join('\n')).replace(/\n/g, '<br>')}</p>`)
    paragraph = []
  }

  while (index < lines.length) {
    const line = lines[index]
    if (!line.trim()) {
      flushParagraph()
      index += 1
      continue
    }

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
