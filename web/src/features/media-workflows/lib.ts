const ROLE_LABEL_KEYS: Record<string, string> = {
  prompt: 'Prompt',
  negative_prompt: 'Negative prompt',
  image: 'Image',
  video: 'Video',
  audio: 'Audio',
  duration: 'Duration',
  aspect_ratio: 'Aspect ratio',
  width: 'Width',
  height: 'Height',
  seed: 'Seed',
  last_frame: 'Last frame',
  fixed: 'Fixed value',
  text: 'Other text',
}

export function roleLabelKey(role: string): string {
  return ROLE_LABEL_KEYS[role] ?? role
}
