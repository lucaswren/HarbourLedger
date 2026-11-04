export interface CompanySettings {
 company: string
 company_aliases: string
 director_names: string
 auto_match_exact: boolean
}

export function companySettings(value: Record<string, unknown> = {}): CompanySettings {
 return {
  company: String(value.company || ''),
  company_aliases: String(value.company_aliases || ''),
  director_names: String(value.director_names || ''),
  auto_match_exact: value.auto_match_exact === 'true' || value.auto_match_exact === true,
 }
}
