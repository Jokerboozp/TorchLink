// tokens.css 是唯一来源：构建时读取其中的变量，生成 Naive UI 主题，避免两处各写一份颜色。
export function parseTokens(css) {
  const tokens = {}
  for (const match of css.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) tokens[match[1]] = match[2].trim()
  return tokens
}

export function resolveToken(tokens, name) {
  let value = tokens[name]
  for (let depth = 0; value?.startsWith('var(') && depth < 8; depth++) value = tokens[value.slice(4, -1).trim()]
  if (value == null) throw new Error(`设计变量 ${name} 未定义`)
  return value
}

export function createThemeOverrides(css) {
  const tokens = parseTokens(css)
  const t = name => resolveToken(tokens, name)
  const statusColors = (key, name) => ({
    [`${key}Color`]: t(`--${name}`),
    [`${key}ColorHover`]: t(`--${name}-hover`),
    [`${key}ColorPressed`]: t(`--${name}-pressed`),
    [`${key}ColorSuppl`]: t(`--${name}-hover`)
  })
  return {
    common: {
      fontFamily: t('--font-sans'),
      fontFamilyMono: t('--font-mono'),
      fontWeightStrong: t('--font-weight-semibold'),
      fontSize: t('--font-size-md'),
      fontSizeMini: t('--font-size-xs'),
      fontSizeTiny: t('--font-size-xs'),
      fontSizeSmall: t('--font-size-sm'),
      fontSizeMedium: t('--font-size-md'),
      fontSizeLarge: '15px',
      fontSizeHuge: t('--font-size-lg'),
      lineHeight: t('--line-height-normal'),
      borderRadius: t('--radius-md'),
      borderRadiusSmall: t('--radius-sm'),
      heightTiny: '24px',
      heightSmall: '28px',
      heightMedium: '34px',
      heightLarge: '40px',
      primaryColor: t('--primary'),
      primaryColorHover: t('--primary-hover'),
      primaryColorPressed: t('--primary-pressed'),
      primaryColorSuppl: t('--primary-hover'),
      ...statusColors('info', 'info'),
      ...statusColors('success', 'success'),
      ...statusColors('warning', 'warning'),
      ...statusColors('error', 'danger'),
      textColorBase: t('--text-strong'),
      textColor1: t('--text-strong'),
      textColor2: t('--text'),
      textColor3: t('--text-muted'),
      textColorDisabled: t('--text-disabled'),
      placeholderColor: t('--text-disabled'),
      placeholderColorDisabled: t('--text-disabled'),
      iconColor: t('--text-muted'),
      borderColor: t('--border-strong'),
      dividerColor: t('--border'),
      bodyColor: t('--bg'),
      cardColor: t('--surface'),
      modalColor: t('--surface'),
      popoverColor: t('--surface'),
      tableColor: t('--surface'),
      tableHeaderColor: t('--surface-muted'),
      tableColorHover: t('--surface-hover'),
      tableColorStriped: t('--surface-muted'),
      hoverColor: t('--surface-hover'),
      actionColor: t('--surface-muted'),
      inputColorDisabled: t('--surface-muted'),
      tagColor: t('--surface-hover'),
      codeColor: t('--code-inline-bg'),
      boxShadow1: t('--shadow-sm'),
      boxShadow2: t('--shadow-lg'),
      boxShadow3: t('--shadow-lg')
    },
    Button: {
      fontWeight: t('--font-weight-medium'),
      textColor: t('--text'),
      border: `1px solid ${t('--border-strong')}`,
      borderHover: `1px solid ${t('--primary-border')}`,
      borderPressed: `1px solid ${t('--primary')}`,
      borderFocus: `1px solid ${t('--primary-border')}`,
      textColorHover: t('--primary-text'),
      textColorPressed: t('--primary'),
      textColorFocus: t('--primary-text'),
      colorHover: t('--primary-soft'),
      colorPressed: t('--primary-soft-hover'),
      colorFocus: t('--primary-soft')
    },
    Card: {
      borderRadius: t('--radius-lg'),
      borderColor: t('--border'),
      titleTextColor: t('--text-strong'),
      titleFontWeight: t('--font-weight-semibold'),
      titleFontSizeSmall: t('--font-size-md'),
      titleFontSizeMedium: '15px',
      paddingSmall: '12px 16px',
      paddingMedium: '16px 20px 20px'
    },
    DataTable: {
      fontSizeSmall: t('--font-size-sm'),
      fontSizeMedium: t('--font-size-sm'),
      thColor: t('--surface-muted'),
      thTextColor: t('--text-secondary'),
      thFontWeight: t('--font-weight-semibold'),
      tdTextColor: t('--text'),
      tdColorHover: t('--surface-hover'),
      tdColorStriped: t('--surface-muted'),
      borderColor: t('--border'),
      thPaddingMedium: '10px 12px',
      tdPaddingMedium: '10px 12px',
      thPaddingSmall: '8px 10px',
      tdPaddingSmall: '8px 10px'
    },
    Tag: {
      borderRadius: t('--radius-sm'),
      fontSizeSmall: t('--font-size-xs')
    },
    Form: {
      labelFontSizeTopMedium: t('--font-size-sm'),
      labelFontSizeLeftMedium: t('--font-size-sm'),
      labelTextColor: t('--text-secondary'),
      labelFontWeight: t('--font-weight-medium')
    },
    Tabs: {
      tabTextColorLine: t('--text-secondary'),
      tabTextColorActiveLine: t('--primary'),
      tabTextColorHoverLine: t('--primary-text'),
      barColor: t('--primary'),
      tabFontWeightActive: t('--font-weight-semibold')
    },
    Modal: { peers: { Card: { borderRadius: t('--radius-xl'), paddingMedium: '20px 24px 24px' } } },
    Dialog: { borderRadius: t('--radius-xl'), titleFontSize: t('--font-size-lg') },
    Drawer: { titleFontSize: t('--font-size-lg'), titleFontWeight: t('--font-weight-semibold'), headerPadding: '16px 24px', bodyPadding: '20px 24px' },
    Descriptions: { thColor: t('--surface-muted'), thTextColor: t('--text-secondary'), tdTextColor: t('--text'), borderColor: t('--border') },
    Pagination: { itemBorderRadius: t('--radius-md') },
    Alert: { borderRadius: t('--radius-lg') }
  }
}

