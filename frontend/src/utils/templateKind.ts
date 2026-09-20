import type { Template } from '../services/api'

// Template IDs never carry a reliable "windows" marker: user-registered custom
// images get generated IDs such as `custom-kvm-42e957647c`, so an ID substring
// match misclassifies them as Linux. The API's `distro` field is the source of
// truth, so every fetched template list registers its Windows entries here.
const windowsTemplateIDs = new Set<string>()

export function registerTemplateKinds(templates: Template[]) {
  for (const template of templates) {
    if (!template?.id) continue
    if ((template.distro || '').toLowerCase() === 'windows') {
      windowsTemplateIDs.add(template.id)
    } else {
      windowsTemplateIDs.delete(template.id)
    }
  }
}

export function isWindowsTemplate(templateID: string) {
  const id = (templateID || '').trim()
  if (!id) return false
  if (windowsTemplateIDs.has(id)) return true
  return id.toLowerCase().includes('windows')
}
