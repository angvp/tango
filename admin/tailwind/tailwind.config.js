/** Tailwind CSS v3 configuration for admin/assets/admin.css. See scripts/admin-css.sh. */

// The semantic colour tokens (admin/theme_palette_test.go lists the same set).
const OPACITY_TOKENS = [
  'ink', 'ink-muted', 'ink-faint',
  'surface', 'surface-sunken', 'surface-hover',
  'line', 'line-subtle', 'line-strong', 'control-line',
  'accent', 'accent-hover', 'accent-fg', 'accent-ui', 'ring', 'selection-bg', 'selection-fg',
  'danger-solid', 'danger-solid-hover', 'danger-solid-fg',
  'sidebar', 'sidebar-fg', 'sidebar-strong', 'sidebar-muted', 'sidebar-dot', 'sidebar-accent', 'sidebar-accent-fg', 'sidebar-pill-hover', 'sidebar-pill-current',
  'success-bg', 'success-fg', 'success-border',
  'danger-bg', 'danger-fg', 'danger-border',
  'warning-bg', 'warning-fg', 'warning-border',
  'neutral-bg', 'neutral-fg', 'neutral-border',
];

// Tailwind's default opacity scale.
const OPACITY_STEPS = ['0', '5', '10', '20', '25', '30', '40', '50', '60', '70', '75', '80', '90', '95', '100'];

module.exports = {
  content: {
    relative: true,
    files: ['../*.go', '!../*_test.go', '../assets/*.js'],
  },
  theme: {
    extend: {
      // The one corner radius of a card; components name it, nothing sizes a radius ad hoc.
      borderRadius: { 'card': '18px' },
      colors: {
      'ink': 'rgb(var(--ink) / <alpha-value>)',
      'ink-muted': 'rgb(var(--ink-muted) / <alpha-value>)',
      'ink-faint': 'rgb(var(--ink-faint) / <alpha-value>)',
      'surface': 'rgb(var(--surface) / <alpha-value>)',
      'surface-sunken': 'rgb(var(--surface-sunken) / <alpha-value>)',
      'surface-hover': 'rgb(var(--surface-hover) / <alpha-value>)',
      'line': 'rgb(var(--line) / <alpha-value>)',
      'line-subtle': 'rgb(var(--line-subtle) / <alpha-value>)',
      'line-strong': 'rgb(var(--line-strong) / <alpha-value>)',
      'control-line': 'rgb(var(--control-line) / <alpha-value>)',
      'accent': 'rgb(var(--accent) / <alpha-value>)',
      'accent-hover': 'rgb(var(--accent-hover) / <alpha-value>)',
      'accent-fg': 'rgb(var(--accent-fg) / <alpha-value>)',
      'accent-ui': 'rgb(var(--accent-ui) / <alpha-value>)',
      'ring': 'rgb(var(--ring) / <alpha-value>)',
      'selection-bg': 'rgb(var(--selection-bg) / <alpha-value>)',
      'selection-fg': 'rgb(var(--selection-fg) / <alpha-value>)',
      'danger-solid': 'rgb(var(--danger-solid) / <alpha-value>)',
      'danger-solid-hover': 'rgb(var(--danger-solid-hover) / <alpha-value>)',
      'danger-solid-fg': 'rgb(var(--danger-solid-fg) / <alpha-value>)',
      'sidebar': 'rgb(var(--sidebar) / <alpha-value>)',
      'sidebar-fg': 'rgb(var(--sidebar-fg) / <alpha-value>)',
      'sidebar-strong': 'rgb(var(--sidebar-strong) / <alpha-value>)',
      'sidebar-muted': 'rgb(var(--sidebar-muted) / <alpha-value>)',
      'sidebar-dot': 'rgb(var(--sidebar-dot) / <alpha-value>)',
      'sidebar-accent': 'rgb(var(--sidebar-accent) / <alpha-value>)',
      'sidebar-accent-fg': 'rgb(var(--sidebar-accent-fg) / <alpha-value>)',
      'sidebar-pill-hover': 'rgb(var(--sidebar-pill-hover) / <alpha-value>)',
      'sidebar-pill-current': 'rgb(var(--sidebar-pill-current) / <alpha-value>)',
      'success-bg': 'rgb(var(--success-bg) / <alpha-value>)',
      'success-fg': 'rgb(var(--success-fg) / <alpha-value>)',
      'success-border': 'rgb(var(--success-border) / <alpha-value>)',
      'danger-bg': 'rgb(var(--danger-bg) / <alpha-value>)',
      'danger-fg': 'rgb(var(--danger-fg) / <alpha-value>)',
      'danger-border': 'rgb(var(--danger-border) / <alpha-value>)',
      'warning-bg': 'rgb(var(--warning-bg) / <alpha-value>)',
      'warning-fg': 'rgb(var(--warning-fg) / <alpha-value>)',
      'warning-border': 'rgb(var(--warning-border) / <alpha-value>)',
      'neutral-bg': 'rgb(var(--neutral-bg) / <alpha-value>)',
      'neutral-fg': 'rgb(var(--neutral-fg) / <alpha-value>)',
      'neutral-border': 'rgb(var(--neutral-border) / <alpha-value>)',
      },
    },
  },
  // Host widgets write the token classes the admin guide documents, and the
  // content scan cannot see their markup, so every token's colour utilities
  // are always generated.
  safelist: [
    {
      pattern: /^(text|bg|border|ring)-(ink|surface|line|control-line|accent|ring|selection|danger-solid|sidebar|success|danger|warning|neutral)(-[a-z]+)*$/,
    },
    // Opacity modifiers (text-ink/60) on the same token utilities. The steps
    // are Tailwind's default opacity scale, listed explicitly so the set does
    // not depend on the theme.
    {
      pattern: new RegExp(
        `^(text|bg|border|ring)-(${OPACITY_TOKENS.join('|')})\\/(${OPACITY_STEPS.join('|')})$`,
      ),
    },
  ],
  plugins: [],
};
