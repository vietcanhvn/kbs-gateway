/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// A model description can carry several languages, one block per language,
// each starting with a marker at the beginning of a line:
//
//   [vi] Tính tiền theo giây video: khoảng 11.400đ/giây...
//   [en] Billed per second of video: $0.44/s...
//
// The page shows only the block of the interface language (falling back to
// English, then to the first block). Text without markers is shown as is.

const LANGUAGE_MARKER = /^\[([a-zA-Z]{2,3}(?:-[a-zA-Z]{2,4})?)\][ \t]*/gm

export function localizeModelDescription(
  description: string | null | undefined,
  language: string
): string {
  const text = description ?? ''
  const markers = [...text.matchAll(LANGUAGE_MARKER)]
  if (markers.length === 0) return text

  const blocks = markers.map((marker, index) => {
    const start = (marker.index ?? 0) + marker[0].length
    const end = markers[index + 1]?.index ?? text.length
    return {
      language: marker[1].toLowerCase(),
      text: text.slice(start, end).trim(),
    }
  })
  const wanted = language.toLowerCase()
  const base = wanted.split('-')[0]
  const pick =
    blocks.find((block) => block.language === wanted) ??
    blocks.find((block) => block.language.split('-')[0] === base) ??
    blocks.find((block) => block.language === 'en') ??
    blocks[0]
  return pick.text
}
