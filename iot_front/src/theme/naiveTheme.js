// Naive UI and page CSS share the same Radix values and layout tokens.
export function parseTokens(css) {
  const tokens = {}
  for (const match of css.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) tokens[match[1]] = match[2].trim()
  return tokens
}

export function resolveToken(tokens, name, seen = new Set()) {
  if (seen.has(name)) throw new Error(`设计变量 ${name} 存在循环引用`)
  const value = tokens[name]
  if (value == null) throw new Error(`设计变量 ${name} 未定义`)
  const references = new Set(seen).add(name)
  return value.replace(/var\(\s*(--[\w-]+)\s*\)/g, (_, reference) => resolveToken(tokens, reference, references))
}

export function createThemeOverrides(css) {
  const tokens = parseTokens(css)
  const t = name => resolveToken(tokens, name)
  const border = name => `${t('--border-width')} solid ${t(name)}`
  const types = { Primary: 'primary', Info: 'info', Success: 'success', Warning: 'warning', Error: 'danger' }
  const byType = create => Object.assign({}, ...Object.entries(types).map(([type, name]) => create(type, name)))
  const statusColors = (key, name) => ({
    [`${key}Color`]: t(`--${name}`),
    [`${key}ColorHover`]: t(`--${name}-hover`),
    [`${key}ColorPressed`]: t(`--${name}-pressed`),
    [`${key}ColorSuppl`]: t(`--${name}-hover`)
  })
  const inputBorders = {
    border: border('--border-strong'),
    borderDisabled: border('--border'),
    borderHover: border('--border-hover'),
    borderFocus: border('--primary-border'),
    borderActive: border('--primary-border'),
    boxShadowFocus: t('--focus-ring'),
    boxShadowActive: t('--focus-ring'),
    ...Object.assign(
      {},
      ...[
        ['Warning', 'warning'],
        ['Error', 'danger']
      ].map(([type, name]) => ({
        [`border${type}`]: border(`--${name}-border`),
        [`borderHover${type}`]: border(`--${name}-border`),
        [`borderFocus${type}`]: border(`--${name}-border`),
        [`borderActive${type}`]: border(`--${name}-border`),
        [`boxShadowFocus${type}`]: t(`--focus-ring-${name}`),
        [`boxShadowActive${type}`]: t(`--focus-ring-${name}`)
      }))
    )
  }
  const theme = {
    common: {
      fontFamily: t('--font-sans'),
      fontFamilyMono: t('--font-mono'),
      fontWeightStrong: t('--font-weight-semibold'),
      fontSize: t('--font-size-md'),
      fontSizeMini: t('--font-size-xs'),
      fontSizeTiny: t('--font-size-xs'),
      fontSizeSmall: t('--font-size-sm'),
      fontSizeMedium: t('--font-size-md'),
      fontSizeLarge: t('--font-size-md'),
      fontSizeHuge: t('--font-size-lg'),
      lineHeight: t('--line-height-normal'),
      borderRadius: t('--radius-md'),
      borderRadiusSmall: t('--radius-sm'),
      heightTiny: '24px',
      heightSmall: '28px',
      heightMedium: '34px',
      heightLarge: '40px',
      bezier: 'cubic-bezier(0, 0, 0.58, 1)',
      bezierEaseOut: 'cubic-bezier(0, 0, 0.58, 1)',
      baseColor: t('--surface'),
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
      placeholderColor: t('--text-muted'),
      placeholderColorDisabled: t('--text-disabled'),
      iconColor: t('--text-muted'),
      iconColorHover: t('--text-strong'),
      iconColorPressed: t('--text-strong'),
      iconColorDisabled: t('--text-disabled'),
      closeIconColor: t('--text-muted'),
      closeIconColorHover: t('--text-strong'),
      closeIconColorPressed: t('--text-strong'),
      closeColorHover: t('--surface-hover'),
      closeColorPressed: t('--surface-pressed'),
      clearColor: t('--text-muted'),
      clearColorHover: t('--text-strong'),
      clearColorPressed: t('--text-strong'),
      scrollbarColor: t('--sand-a6'),
      scrollbarColorHover: t('--sand-a8'),
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
      pressedColor: t('--surface-pressed'),
      actionColor: t('--surface-muted'),
      inputColor: t('--surface'),
      inputColorDisabled: t('--surface-muted'),
      buttonColor2: t('--surface-sunken'),
      buttonColor2Hover: t('--surface-hover'),
      buttonColor2Pressed: t('--surface-pressed'),
      tagColor: t('--surface-sunken'),
      avatarColor: t('--surface-pressed'),
      tabColor: t('--surface-sunken'),
      invertedColor: t('--code-bg'),
      railColor: t('--sand-6'),
      progressRailColor: t('--sand-4'),
      codeColor: t('--code-inline-bg'),
      boxShadow1: t('--shadow-sm'),
      boxShadow2: t('--shadow-lg'),
      boxShadow3: t('--shadow-lg')
    },
    Button: {
      fontWeight: t('--font-weight-medium'),
      textColor: t('--text'),
      border: border('--border-strong'),
      borderDisabled: border('--border'),
      borderHover: border('--border-hover'),
      borderPressed: border('--border-hover'),
      borderFocus: border('--border-hover'),
      textColorHover: t('--text-strong'),
      textColorPressed: t('--text-strong'),
      textColorFocus: t('--text-strong'),
      textColorTextHover: t('--text-strong'),
      textColorTextPressed: t('--text-strong'),
      textColorTextFocus: t('--text-strong'),
      textColorGhostHover: t('--text-strong'),
      textColorGhostPressed: t('--text-strong'),
      textColorGhostFocus: t('--text-strong'),
      color: t('--surface'),
      colorHover: t('--surface-hover'),
      colorPressed: t('--surface-pressed'),
      colorFocus: t('--surface-hover'),
      ...byType((type, name) =>
        Object.assign(
          {},
          ...['', 'Hover', 'Pressed', 'Focus', 'Disabled'].map(state => ({
            [`textColor${state}${type}`]: t(name === 'warning' ? '--warning-contrast' : '--text-inverse'),
            [`textColorText${state}${type}`]: t(`--${name}-text`),
            [`textColorGhost${state}${type}`]: t(`--${name}-text`),
            [`border${state}${type}`]: border(`--${name}${state === 'Hover' || state === 'Focus' || state === 'Pressed' ? '-hover' : ''}`)
          }))
        )
      )
    },
    Input: { ...inputBorders, groupLabelBorder: border('--border-strong') },
    InternalSelection: inputBorders,
    InternalSelectMenu: {
      optionColorActive: t('--primary-soft-active'),
      optionColorActivePending: t('--primary-soft-hover'),
      optionTextColorActive: t('--primary-text'),
      optionTextColorPressed: t('--primary-text'),
      optionCheckColor: t('--primary-text')
    },
    Dropdown: {
      borderRadius: t('--radius-lg'),
      optionColorHover: t('--surface-hover'),
      optionColorActive: t('--primary-soft-active'),
      optionTextColor: t('--text'),
      optionTextColorActive: t('--primary-text'),
      optionTextColorChildActive: t('--primary-text'),
      padding: '4px'
    },
    Popover: { borderRadius: t('--radius-lg') },
    Tooltip: { color: t('--code-bg'), textColor: t('--code-text'), borderRadius: t('--radius-sm') },
    Skeleton: { color: t('--sand-3'), colorEnd: t('--sand-4') },
    Empty: { textColor: t('--text-muted') },
    Radio: {
      boxShadow: `inset 0 0 0 ${t('--border-width')} ${t('--border-strong')}`,
      boxShadowActive: `inset 0 0 0 ${t('--border-width')} ${t('--primary')}`,
      boxShadowHover: `inset 0 0 0 ${t('--border-width')} ${t('--border-hover')}`,
      boxShadowDisabled: `inset 0 0 0 ${t('--border-width')} ${t('--border')}`,
      boxShadowFocus: `inset 0 0 0 ${t('--border-width')} ${t('--primary')}, ${t('--focus-ring')}`,
      buttonBoxShadowFocus: t('--focus-ring'),
      buttonBoxShadowHover: 'none',
      buttonBoxShadow: 'none',
      buttonTextColorActive: t('--primary-text'),
      buttonColorActive: t('--primary-soft-active'),
      buttonBorderColorActive: t('--primary-border'),
      buttonTextColorHover: t('--text-strong')
    },
    Checkbox: {
      border: border('--border-strong'),
      borderDisabled: border('--border'),
      borderDisabledChecked: border('--border'),
      borderChecked: border('--primary'),
      borderFocus: border('--primary-border'),
      checkMarkColor: t('--text-inverse'),
      boxShadowFocus: t('--focus-ring')
    },
    Switch: {
      railColor: t('--sand-7'),
      railColorActive: t('--primary'),
      buttonColor: t('--radix-white'),
      textColor: t('--text-inverse'),
      buttonBoxShadow: 'none',
      boxShadowFocus: t('--focus-ring')
    },
    Card: {
      borderRadius: t('--radius-lg'),
      borderColor: t('--border'),
      boxShadow: 'none',
      titleTextColor: t('--text-strong'),
      titleFontWeight: t('--font-weight-semibold'),
      titleFontSizeSmall: t('--font-size-md'),
      titleFontSizeMedium: t('--font-size-md'),
      titleFontSizeLarge: t('--font-size-lg'),
      titleFontSizeHuge: t('--font-size-lg'),
      paddingSmall: '12px 16px',
      paddingMedium: '16px 20px 20px'
    },
    DataTable: {
      fontSizeSmall: t('--font-size-sm'),
      fontSizeMedium: t('--font-size-md'),
      fontSizeLarge: t('--font-size-md'),
      thColor: t('--surface-muted'),
      thColorHover: t('--surface-hover'),
      thColorSorting: t('--surface-pressed'),
      thTextColor: t('--text-secondary'),
      thFontWeight: t('--font-weight-medium'),
      tdTextColor: t('--text'),
      tdColorHover: t('--surface-hover'),
      tdColorStriped: t('--surface-muted'),
      tdColorSorting: t('--surface-pressed'),
      borderColor: t('--border'),
      boxShadowBefore: 'none',
      boxShadowAfter: 'none',
      thPaddingMedium: '10px 12px',
      tdPaddingMedium: '10px 12px',
      thPaddingSmall: '8px 10px',
      tdPaddingSmall: '8px 10px'
    },
    Tag: {
      borderRadius: t('--radius-sm'),
      fontSizeSmall: t('--font-size-xs'),
      border: border('--border'),
      colorBordered: t('--surface-sunken'),
      ...byType((type, name) => ({
        [`textColor${type}`]: t(`--${name}-text`),
        [`color${type}`]: t(`--${name}-soft`),
        [`colorBordered${type}`]: t(`--${name}-soft`),
        [`border${type}`]: border(`--${name}-border`),
        [`closeIconColor${type}`]: t(`--${name}-text`),
        [`closeIconColorHover${type}`]: t(`--${name}-text`),
        [`closeIconColorPressed${type}`]: t(`--${name}-text`),
        [`closeColorHover${type}`]: t(`--${name}-soft-hover`),
        [`closeColorPressed${type}`]: t(`--${name}-soft-active`)
      }))
    },
    Form: {
      labelFontSizeTopMedium: t('--font-size-md'),
      labelFontSizeLeftMedium: t('--font-size-md'),
      labelTextColor: t('--text-secondary'),
      labelFontWeight: t('--font-weight-medium'),
      feedbackTextColorWarning: t('--warning-text'),
      feedbackTextColorError: t('--danger-text')
    },
    Tabs: {
      tabFontSizeSmall: t('--font-size-xs'),
      tabFontSizeMedium: t('--font-size-md'),
      tabFontSizeLarge: t('--font-size-md'),
      tabTextColorLine: t('--text-secondary'),
      tabTextColorActiveLine: t('--text-strong'),
      tabTextColorHoverLine: t('--text-strong'),
      tabTextColorActiveCard: t('--primary-text'),
      tabTextColorHoverCard: t('--text-strong'),
      tabTextColorActiveBar: t('--primary-text'),
      tabTextColorHoverBar: t('--text-strong'),
      barColor: t('--primary'),
      tabFontWeightActive: t('--font-weight-semibold')
    },
    Modal: { peers: { Card: { borderRadius: t('--radius-xl'), paddingMedium: '20px 24px 24px' } } },
    Dialog: { borderRadius: t('--radius-xl'), titleFontSize: t('--font-size-lg') },
    Message: { borderRadius: t('--radius-lg') },
    Notification: { borderRadius: t('--radius-lg'), titleFontSize: t('--font-size-md') },
    Drawer: {
      titleFontSize: t('--font-size-lg'),
      titleFontWeight: t('--font-weight-semibold'),
      headerBorderBottom: border('--border'),
      footerBorderTop: border('--border'),
      headerPadding: '16px 24px',
      bodyPadding: '20px 24px'
    },
    Descriptions: {
      thColor: t('--surface-muted'),
      thTextColor: t('--text-secondary'),
      tdTextColor: t('--text'),
      borderColor: t('--border')
    },
    Progress: {
      fontSizeCircle: t('--font-size-lg'),
      textColorLineInner: t('--text-inverse'),
      lineBgProcessing: `linear-gradient(90deg, ${t('--white-a5')}, ${t('--white-a7')})`
    },
    DatePicker: {
      itemColorIncluded: t('--primary-soft'),
      itemColorActive: t('--primary'),
      itemTextColorActive: t('--text-inverse'),
      itemTextColorCurrent: t('--primary-text')
    },
    Tree: { nodeColorActive: t('--primary-soft-active') },
    Steps: {
      stepHeaderFontSizeSmall: t('--font-size-md'),
      stepHeaderFontSizeMedium: t('--font-size-md'),
      indicatorIndexFontSizeSmall: t('--font-size-md'),
      indicatorIndexFontSizeMedium: t('--font-size-md'),
      indicatorTextColorProcess: t('--text-inverse')
    },
    Upload: {
      draggerBorder: `${t('--border-width')} dashed ${t('--border-strong')}`,
      draggerBorderHover: `${t('--border-width')} dashed ${t('--border-hover')}`,
      itemBorderImageCard: border('--border-strong'),
      itemBorderImageCardError: border('--danger-border'),
      itemColorHoverError: t('--danger-soft'),
      itemTextColorError: t('--danger-text'),
      itemTextColorSuccess: t('--success-text')
    },
    Pagination: {
      itemBorderRadius: t('--radius-md'),
      buttonBorder: border('--border-strong'),
      buttonBorderHover: border('--border-hover'),
      buttonBorderPressed: border('--border-hover'),
      itemBorder: `${t('--border-width')} solid transparent`,
      itemBorderHover: `${t('--border-width')} solid transparent`,
      itemBorderPressed: `${t('--border-width')} solid transparent`,
      itemBorderActive: border('--primary-border'),
      itemBorderDisabled: border('--border'),
      itemColorHover: t('--surface-hover'),
      itemColorPressed: t('--surface-pressed'),
      itemColorActive: t('--primary-soft-active'),
      itemColorActiveHover: t('--primary-soft-hover'),
      itemTextColorHover: t('--text-strong'),
      itemTextColorPressed: t('--text-strong'),
      itemTextColorActive: t('--primary-text')
    },
    Alert: {
      borderRadius: t('--radius-lg'),
      border: border('--border'),
      titleFontSize: t('--font-size-md'),
      ...byType((type, name) => ({
        [`border${type}`]: border(`--${name}-border`),
        [`color${type}`]: t(`--${name}-soft`),
        [`iconColor${type}`]: t(`--${name}-text`),
        [`titleTextColor${type}`]: t(`--${name}-text`),
        [`contentTextColor${type}`]: t('--text')
      }))
    }
  }
  return theme
}
