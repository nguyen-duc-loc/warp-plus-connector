export const theme = {accent: 'cyan', minimumColumns: 60, minimumRows: 16, tableColumns: 100,
  headerRows: 4, footerRows: 2, refreshDelay: 5000} as const;

// Display controls as visible text before width calculation or rendering.
export function safe(text: string): string {
  return text.replace(/[\x00-\x1f\x7f-\x9f\u2028\u2029\u202a-\u202e\u2066-\u2069]/g,
    c => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`);
}
