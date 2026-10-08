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
import { cn } from '@/lib/utils'

interface SiteNameProps {
  name: string
  className?: string
}

/**
 * Tên trang theo kiểu logo KimStudio's Box: tên bắt đầu bằng "KBS" (vd "KBSAPI")
 * thì "KBS" tô xanh như logo KBS6, phần sau đậm màu chữ thường (đen / trắng theo
 * giao diện sáng / tối). Tên khác hiện như cũ.
 */
export function SiteName({ name, className }: SiteNameProps) {
  const match = /^KBS(.*)$/.exec(name)
  if (!match) return <span className={className}>{name}</span>
  return (
    <span className={cn('font-black tracking-tighter', className)}>
      <span className='bg-gradient-to-br from-cyan-400 to-teal-600 bg-clip-text text-transparent'>
        KBS
      </span>
      <span className='ms-[0.06em]'>{match[1]}</span>
    </span>
  )
}
