export interface GroupColorOption {
  id: string;
  label: string;
  swatch: string;
  dot: string;
  chip: string;
  icon: string;
  bar: string;
}

export const GROUP_COLOR_OPTIONS: GroupColorOption[] = [
  { id: "blue", label: "海洋蓝", swatch: "bg-sky-500", dot: "bg-sky-500", chip: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-800/70 dark:bg-sky-950/40 dark:text-sky-300", icon: "bg-sky-500/10 text-sky-600 dark:text-sky-300", bar: "bg-sky-500" },
  { id: "cyan", label: "青绿色", swatch: "bg-cyan-500", dot: "bg-cyan-500", chip: "border-cyan-200 bg-cyan-50 text-cyan-700 dark:border-cyan-800/70 dark:bg-cyan-950/40 dark:text-cyan-300", icon: "bg-cyan-500/10 text-cyan-600 dark:text-cyan-300", bar: "bg-cyan-500" },
  { id: "violet", label: "紫罗兰", swatch: "bg-violet-500", dot: "bg-violet-500", chip: "border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-800/70 dark:bg-violet-950/40 dark:text-violet-300", icon: "bg-violet-500/10 text-violet-600 dark:text-violet-300", bar: "bg-violet-500" },
  { id: "emerald", label: "翡翠绿", swatch: "bg-emerald-500", dot: "bg-emerald-500", chip: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800/70 dark:bg-emerald-950/40 dark:text-emerald-300", icon: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300", bar: "bg-emerald-500" },
  { id: "amber", label: "琥珀黄", swatch: "bg-amber-500", dot: "bg-amber-500", chip: "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800/70 dark:bg-amber-950/40 dark:text-amber-300", icon: "bg-amber-500/10 text-amber-600 dark:text-amber-300", bar: "bg-amber-500" },
  { id: "rose", label: "玫瑰红", swatch: "bg-rose-500", dot: "bg-rose-500", chip: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-800/70 dark:bg-rose-950/40 dark:text-rose-300", icon: "bg-rose-500/10 text-rose-600 dark:text-rose-300", bar: "bg-rose-500" },
];

export function groupColor(color: string): GroupColorOption {
  return GROUP_COLOR_OPTIONS.find((option) => option.id === color) ?? GROUP_COLOR_OPTIONS[0];
}
