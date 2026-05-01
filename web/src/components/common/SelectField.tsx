
import {Label, ListBox, Select} from "@heroui/react";

export const nativeSelectClassName =
  "w-full rounded-md border border-(--line) bg-white px-3 py-2 text-sm";

export type SelectOption = {
  label: string;
  value: string;
};

export function SelectField({
  disabled = false,
  label,
  onChange,
  options,
  value,
}: {
  disabled?: boolean;
  label: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  value: string;
}) {
  return (
    <label className="block text-sm font-medium text-(--ink)">
      {label}
      <select
        className={`${nativeSelectClassName} mt-2`}
        disabled={disabled}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      >
        {options.map((option) => (
          <option key={`${label}-${option.value}`} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}
