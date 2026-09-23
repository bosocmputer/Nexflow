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
      className="toaster group"
      icons={{
        success: <CircleCheck className="h-4 w-4" />,
        info: <Info className="h-4 w-4" />,
        warning: <TriangleAlert className="h-4 w-4" />,
        error: <OctagonX className="h-4 w-4" />,
        loading: <LoaderCircle className="h-4 w-4 animate-spin" />,
      }}
      toastOptions={{
        classNames: {
          toast:
            "group toast !border-border !bg-card !text-card-foreground shadow-lg",
          title: "group-[.toast]:!text-card-foreground",
          description: "group-[.toast]:!text-muted-foreground",
          success: "!border-success/35 !bg-success/10 !text-foreground",
          info: "!border-primary/35 !bg-primary/10 !text-foreground",
          warning: "!border-warning/35 !bg-warning/10 !text-foreground",
          error: "!border-destructive/35 !bg-destructive/10 !text-foreground",
          actionButton:
            "group-[.toast]:!bg-primary group-[.toast]:!text-primary-foreground",
          cancelButton:
            "group-[.toast]:!bg-muted group-[.toast]:!text-muted-foreground",
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
