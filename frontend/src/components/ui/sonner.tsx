import {
  CircleCheck,
  Info,
  LoaderCircle,
  OctagonX,
  TriangleAlert,
} from "lucide-react"
import { useTheme } from "next-themes"
import { Toaster as Sonner } from "sonner"

type ToasterProps = React.ComponentProps<typeof Sonner>

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = "system" } = useTheme()

  return (
    <Sonner
      theme={theme as ToasterProps["theme"]}
      position="bottom-right"
      closeButton
      gap={8}
      visibleToasts={3}
      className="toaster group"
      icons={{
        success: <CircleCheck className="h-4 w-4 text-success" />,
        info: <Info className="h-4 w-4 text-info" />,
        warning: <TriangleAlert className="h-4 w-4 text-warning" />,
        error: <OctagonX className="h-4 w-4 text-destructive" />,
        loading: <LoaderCircle className="h-4 w-4 animate-spin" />,
      }}
      toastOptions={{
        classNames: {
          toast:
            "group toast !border-border !bg-card !text-card-foreground shadow-md",
          title: "group-[.toast]:!text-card-foreground !font-medium !leading-5",
          description: "group-[.toast]:!text-muted-foreground !leading-5",
          // Keep semantic meaning on the border and icon, while leaving the
          // surface opaque. Transparent success/info surfaces made controls
          // behind the toast look like part of the notification.
          success: "!border-success/50 !text-card-foreground",
          info: "!border-info/50 !text-card-foreground",
          warning: "!border-warning/50 !text-card-foreground",
          error: "!border-destructive/50 !text-card-foreground",
          actionButton:
            "group-[.toast]:!shrink-0 group-[.toast]:!bg-primary group-[.toast]:!text-primary-foreground",
          cancelButton:
            "group-[.toast]:!shrink-0 group-[.toast]:!bg-muted group-[.toast]:!text-muted-foreground",
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
