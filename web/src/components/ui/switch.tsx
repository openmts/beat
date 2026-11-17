import type { ButtonHTMLAttributes } from "react";
import { LoaderCircle } from "lucide-react";
import { cn } from "@/lib/utils";

type SwitchProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onChange" | "type"> & {
  checked: boolean;
  label: string;
  loading?: boolean;
  onCheckedChange: (checked: boolean) => void;
};

export function Switch({ checked, label, loading = false, disabled, onCheckedChange, className, ...props }: SwitchProps) {
  return (
    <button
      {...props}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled || loading}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        "inline-flex h-6 w-11 shrink-0 items-center rounded-full border p-[3px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50",
        checked ? "border-primary bg-primary" : "border-input bg-muted",
        className,
      )}
    >
      {loading ? (
        <LoaderCircle className="mx-auto h-4 w-4 animate-spin text-white" />
      ) : (
        <span className={cn("block h-[18px] w-[18px] rounded-full bg-white shadow-sm transition-transform duration-200", checked ? "translate-x-[18px]" : "translate-x-0")} />
      )}
    </button>
  );
}
