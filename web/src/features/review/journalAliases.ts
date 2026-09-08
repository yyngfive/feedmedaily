// Read-time aliases only: never overwrite stored bibliographic metadata.
const acsTitles = new Set([
  "Accounts of Chemical Research", "Accounts of Materials Research",
  "Analytical Chemistry", "Biochemistry", "Bioconjugate Chemistry",
  "Biomacromolecules", "Biotechnology Progress", "Chemical & Engineering News",
  "Chemical Research in Toxicology", "Chemical Reviews", "Chemistry of Materials",
  "Crystal Growth & Design", "Energy & Fuels", "Environmental Science & Technology",
  "Environmental Science & Technology Letters", "Industrial & Engineering Chemistry Research",
  "Inorganic Chemistry", "JACS Au", "Journal of Agricultural and Food Chemistry",
  "Journal of Chemical & Engineering Data", "Journal of Chemical Education",
  "Journal of Chemical Information and Modeling", "Journal of Chemical Theory and Computation",
  "Journal of Medicinal Chemistry", "Journal of Natural Products",
  "Journal of Proteome Research", "Journal of the American Chemical Society",
  "Langmuir", "Macromolecules", "Molecular Pharmaceutics", "Nano Letters",
  "Organic Letters", "Organic Process Research & Development", "Organometallics",
  "The Journal of Organic Chemistry", "The Journal of Physical Chemistry A",
  "The Journal of Physical Chemistry B", "The Journal of Physical Chemistry C",
  "The Journal of Physical Chemistry Letters",
]);

export function journalAlias(journal: string | null | undefined): string {
  if (!journal) return "";
  const candidate = journal.trim();
  const advance = /^(.*?)\s+advanceAccess$/i.exec(candidate);
  if (advance && (/^ACS\s+\S/.test(advance[1]) || acsTitles.has(advance[1]))) return advance[1];
  if (/^Cell,\s*Volume\s+\d+,\s*Issue\s+\d+(?:\s*[-–]\s*\d+)?$/i.test(candidate)) return "Cell";
  return journal;
}

export function journalSelection(values: string[]): string[] {
  return Array.from(new Set(values.map(journalAlias))).sort();
}

export function toggleJournalSelection(values: string[], value: string): string[] {
  const current = journalSelection(values);
  const alias = journalAlias(value);
  return current.includes(alias) ? current.filter((item) => item !== alias) : [...current, alias].sort();
}
