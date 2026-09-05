import React, { type InputHTMLAttributes, type ReactNode } from "react";
import {
  Checkbox,
  CloseButton,
  Description,
  Input,
  InputGroup,
  Label,
  TextArea,
  TextField,
} from "@heroui/react";

type TextInputFieldProps = {
  className?: string;
  clearable?: boolean;
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

type ScrollSnapshot = {
  element: HTMLElement;
  left: number;
  top: number;
};

function captureScrollParents(node: HTMLElement | null): ScrollSnapshot[] {
  const snapshots: ScrollSnapshot[] = [];
  const seen = new Set<HTMLElement>();
  for (let current = node?.parentElement ?? null; current; current = current.parentElement) {
    if (current.scrollHeight > current.clientHeight || current.scrollWidth > current.clientWidth) {
      snapshots.push({ element: current, left: current.scrollLeft, top: current.scrollTop });
      seen.add(current);
    }
  }
  const root = document.scrollingElement;
  if (root instanceof HTMLElement && !seen.has(root)) {
    snapshots.push({ element: root, left: root.scrollLeft, top: root.scrollTop });
  }
  return snapshots;
}

function restoreScrollParents(snapshots: ScrollSnapshot[]) {
  for (const item of snapshots) {
    item.element.scrollTo({ left: item.left, top: item.top });
  }
}

export function TextInputField({
  className = "",
  clearable = false,
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

      {clearable ? (
        <InputGroup fullWidth>
          <InputGroup.Input
            inputMode={inputMode}
            placeholder={placeholder}
          />

          {value ? (
            <InputGroup.Suffix>
              <CloseButton
                aria-label={`Clear ${label}`}
                isDisabled={disabled}
                onPress={() => onChange("")}
              />
            </InputGroup.Suffix>
          ) : null}
        </InputGroup>
      ) : (
        <Input
          className="w-full"
          inputMode={inputMode}
          placeholder={placeholder}
        />
      )}

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
  const rootRef = React.useRef<HTMLLabelElement | null>(null);
  const scrollSnapshotRef = React.useRef<ScrollSnapshot[]>([]);
  const captureScroll = () => {
    scrollSnapshotRef.current = captureScrollParents(rootRef.current);
  };
  const restoreScroll = () => restoreScrollParents(scrollSnapshotRef.current);
  const handleChange = (nextChecked: boolean) => {
    if (scrollSnapshotRef.current.length === 0) {
      captureScroll();
    }
    onChange(nextChecked);
    queueMicrotask(restoreScroll);
    window.requestAnimationFrame(restoreScroll);
    window.setTimeout(restoreScroll, 0);
    window.setTimeout(restoreScroll, 50);
  };

  return (
    <Checkbox
      ref={rootRef}
      className={`w-full ${className}`}
      isDisabled={disabled}
      isSelected={checked}
      onChange={handleChange}
      onPointerDownCapture={captureScroll}
    >
      <Checkbox.Content className="flex! flex-row! items-start gap-2 text-left">
        <Checkbox.Control className="mt-1 shrink-0">
          <Checkbox.Indicator />
        </Checkbox.Control>
        {children}
      </Checkbox.Content>
    </Checkbox>
  );
}
