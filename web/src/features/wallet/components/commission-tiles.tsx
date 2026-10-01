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

/** A row of labelled numbers, same look as the wallet stat tiles. */
export function CommissionTiles(props: {
  items: Array<{ label: string; value: string }>
  className?: string
}) {
  return (
    <div
      className={cn(
        'grid grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-4',
        props.className
      )}
    >
      {props.items.map((item) => (
        <div key={item.label} className='bg-muted/20 p-3'>
          <div className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
            {item.label}
          </div>
          <div className='mt-1 truncate text-base font-semibold tabular-nums'>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  )
}
