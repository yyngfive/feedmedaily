import type {Key} from "react";
import {Label, ListBox, ListBoxItem, Select} from "@heroui/react";

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
    <div className="space-y-2">
      <Label>{label}</Label>
      <Select
        aria-label={label}
        isDisabled={disabled}
        selectedKey={value}
        onSelectionChange={(key: Key | null) => onChange(String(key ?? ""))}
      >
        <Select.Trigger>
          <Select.Value />
          <Select.Indicator />
        </Select.Trigger>
        <Select.Popover>
          <ListBox aria-label={label}>
            {options.map((option) => (
              <ListBoxItem key={option.value} id={option.value} textValue={option.label}>
                {option.label}
              </ListBoxItem>
            ))}
          </ListBox>
        </Select.Popover>
      </Select>
    </div>
  );
}
