import type {InputHTMLAttributes, ReactNode} from "react";
import {Checkbox, Description, Input, Label, TextArea, TextField} from "@heroui/react";

type TextInputFieldProps = {
  className?: string;
  description?: ReactNode;
  disabled?: boolean;
  hideLabel?: boolean;
  inputMode?: InputHTMLAttributes<HTMLInputElement>["inputMode"];
  label: string;
  placeholder?: string;
  type?: "text" | "password" | "url" | "number";
  value: string;
  onChange: (value: string) => void;
};

type TextAreaFieldProps = {
  className?: string;
  description?: ReactNode;
  disabled?: boolean;
  hideLabel?: boolean;
  label: string;
  placeholder?: string;
  rows?: number;
  value: string;
  onChange: (value: string) => void;
};

export function TextInputField({
  className = "",
  description,
  disabled = false,
  hideLabel = false,
  inputMode,
  label,
  onChange,
  placeholder,
  type = "text",
  value,
}: TextInputFieldProps) {
  return (
    <TextField
      className={`w-full gap-2 ${className}`}
      isDisabled={disabled}
      type={type}
      value={value}
      onChange={onChange}
    >
      <Label className={hideLabel ? "sr-only" : undefined}>{label}</Label>
      <Input className="w-full" inputMode={inputMode} placeholder={placeholder} />
      {description ? <Description>{description}</Description> : null}
    </TextField>
  );
}

export function TextAreaField({
  className = "",
  description,
  disabled = false,
  hideLabel = false,
  label,
  onChange,
  placeholder,
  rows,
  value,
}: TextAreaFieldProps) {
  return (
    <TextField
      className={`w-full gap-2 ${className}`}
      isDisabled={disabled}
      value={value}
      onChange={onChange}
    >
      <Label className={hideLabel ? "sr-only" : undefined}>{label}</Label>
      <TextArea className="w-full" placeholder={placeholder} rows={rows} style={{resize: "vertical"}} />
      {description ? <Description>{description}</Description> : null}
    </TextField>
  );
}

export function CheckboxRow({
  checked,
  children,
  className = "",
  disabled = false,
  onChange,
}: {
  checked: boolean;
  children: ReactNode;
  className?: string;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <Checkbox
      className={`w-full ${className}`}
      isDisabled={disabled}
      isSelected={checked}
      onChange={onChange}
    >
      <Checkbox.Content className="!flex !flex-row items-start gap-2 text-left">
        <Checkbox.Control className="mt-1 shrink-0">
          <Checkbox.Indicator />
        </Checkbox.Control>
        {children}
      </Checkbox.Content>
    </Checkbox>
  );
}
