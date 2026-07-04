import type {Key, ReactNode} from "react";
import {Description, Label, ListBox, ListBoxItem, Select} from "@heroui/react";

export type SelectOption = {
  label: string;
  value: string;
};

export function SelectField({
  className = "",
  description,
  disabled = false,
  hideLabel = false,
  id,
  label,
  onChange,
  options,
  value,
}: {
  className?: string;
  description?: ReactNode;
  disabled?: boolean;
  hideLabel?: boolean;
  id?: string;
  label: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  value: string;
}) {
  return (
    <div className={`space-y-2 ${className}`}>
      <Label className={hideLabel ? "sr-only" : undefined}>{label}</Label>
      <Select
        aria-label={label}
        id={id}
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
      {description ? <Description>{description}</Description> : null}
    </div>
  );
}
