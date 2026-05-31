import { useState } from 'react'
import {
  AlertCircle,
  CheckCircle2,
  FileText,
  Inbox,
  Search,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'

import { StatusDot } from '@/components/common/StatusDot'
import { KeyboardShortcut } from '@/components/common/KeyboardShortcut'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import { JsonViewer } from '@/components/common/JsonViewer'
import { DataTable } from '@/components/common/DataTable'
import {
  CardSkeleton,
  RowSkeleton,
  StatCardSkeleton,
} from '@/components/common/LoadingSkeleton'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ThemeToggle } from '@/components/common/ThemeToggle'

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h2>
      <div className="rounded-lg border border-border bg-card p-6">{children}</div>
    </section>
  )
}

export default function Showcase() {
  const [confirmOpen, setConfirmOpen] = useState(false)

  return (
    <TooltipProvider>
      <div className="mx-auto max-w-6xl space-y-8 p-8">
        <PageHeader
          title="Component Showcase"
          description="QA หน้าทุก primitive ใน light + dark mode — ใช้ DevTools toggle <html class='dark'> เพื่อตรวจ"
          actions={<ThemeToggle />}
        />

        <Section title="Buttons">
          <div className="flex flex-wrap gap-2">
            <Button>Default</Button>
            <Button variant="secondary">Secondary</Button>
            <Button variant="outline">Outline</Button>
            <Button variant="ghost">Ghost</Button>
            <Button variant="link">Link</Button>
            <Button variant="destructive">Destructive</Button>
            <Button size="sm">Small</Button>
            <Button size="lg">Large</Button>
            <Button disabled>Disabled</Button>
          </div>
        </Section>

        <Section title="Inputs / Form">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label>อีเมล</Label>
              <Input placeholder="your.email@company.com" />
            </div>
            <div className="space-y-2">
              <Label>หน่วย</Label>
              <Select>
                <SelectTrigger>
                  <SelectValue placeholder="เลือก..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="bag">ถุง</SelectItem>
                  <SelectItem value="piece">เส้น</SelectItem>
                  <SelectItem value="set">ชุด</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="md:col-span-2 space-y-2">
              <Label>หมายเหตุ</Label>
              <Textarea placeholder="พิมพ์อะไรก็ได้..." />
            </div>
            <div className="flex items-center gap-2">
              <Checkbox id="cb1" />
              <Label htmlFor="cb1">Checkbox</Label>
            </div>
            <div className="flex items-center gap-2">
              <Switch id="sw1" />
              <Label htmlFor="sw1">Switch</Label>
            </div>
          </div>
        </Section>

        <Section title="Status / Badges / StatusDot">
          <div className="flex flex-wrap items-center gap-3">
            <Badge>Default</Badge>
            <Badge variant="secondary">Secondary</Badge>
            <Badge variant="outline">Outline</Badge>
            <Badge variant="destructive">Destructive</Badge>
            <Separator orientation="vertical" className="h-6" />
            <StatusDot variant="success" label="ส่งสำเร็จ" />
            <StatusDot variant="warning" label="รอตรวจสอบ" />
            <StatusDot variant="danger" label="ล้มเหลว" />
            <StatusDot variant="info" label="เริ่มต้น" />
            <StatusDot variant="muted" label="ข้าม" />
            <StatusDot variant="success" label="กำลังประมวลผล" pulse />
          </div>
        </Section>

        <Section title="Keyboard shortcuts">
          <div className="flex flex-wrap items-center gap-4 text-sm">
            <span>เปิด palette</span>
            <KeyboardShortcut keys="mod+k" />
            <span>ค้นหา</span>
            <KeyboardShortcut keys={['mod', 'shift', 'p']} />
            <span>กลับ</span>
            <KeyboardShortcut keys="escape" />
          </div>
        </Section>

        <Section title="Cards">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <Card>
              <CardHeader>
                <CardTitle>บิลทั้งหมด</CardTitle>
                <CardDescription>เดือนนี้</CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-semibold tabular-nums">124</p>
                <p className="mt-1 text-xs text-muted-foreground">+12% จากเดือนที่แล้ว</p>
              </CardContent>
              <CardFooter>
                <Button variant="ghost" size="sm">ดูทั้งหมด</Button>
              </CardFooter>
            </Card>
            <StatCardSkeleton />
            <CardSkeleton />
          </div>
        </Section>

        <Section title="Alerts">
          <div className="space-y-3">
            <Alert>
              <CheckCircle2 className="h-4 w-4" />
              <AlertTitle>สำเร็จ</AlertTitle>
              <AlertDescription>ระบบได้บันทึกข้อมูลเรียบร้อยแล้ว</AlertDescription>
            </Alert>
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>เกิดข้อผิดพลาด</AlertTitle>
              <AlertDescription>ไม่สามารถส่งบิลไปยัง SML ได้</AlertDescription>
            </Alert>
          </div>
        </Section>

        <Section title="Tabs">
          <Tabs defaultValue="overview">
            <TabsList>
              <TabsTrigger value="overview">ภาพรวม</TabsTrigger>
              <TabsTrigger value="items">รายการสินค้า</TabsTrigger>
              <TabsTrigger value="logs">ประวัติ</TabsTrigger>
            </TabsList>
            <TabsContent value="overview" className="mt-3 text-sm text-muted-foreground">
              ภาพรวมของบิล — สถานะ, จำนวน, ผู้ส่ง
            </TabsContent>
            <TabsContent value="items" className="mt-3 text-sm text-muted-foreground">
              รายการสินค้าทั้งหมดในบิล
            </TabsContent>
            <TabsContent value="logs" className="mt-3 text-sm text-muted-foreground">
              ประวัติการแก้ไขและส่ง SML
            </TabsContent>
          </Tabs>
        </Section>

        <Section title="Table (raw shadcn)">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>รหัสสินค้า</TableHead>
                <TableHead>ชื่อ</TableHead>
                <TableHead className="text-right">ราคา</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell className="font-mono text-xs">CON-01000</TableCell>
                <TableCell>ปูนซีเมนต์</TableCell>
                <TableCell className="text-right tabular-nums">฿120.00</TableCell>
              </TableRow>
              <TableRow>
                <TableCell className="font-mono text-xs">STEEL-A12</TableCell>
                <TableCell>เหล็กเส้น 12 มม.</TableCell>
                <TableCell className="text-right tabular-nums">฿890.00</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </Section>

        <Section title="DataTable (custom wrapper)">
          <DataTable
            dense
            columns={[
              { key: 'doc', header: 'เลขบิล', cell: (r: { doc: string; cust: string; amt: number }) => <span className="font-mono text-xs">{r.doc}</span> },
              { key: 'cust', header: 'ลูกค้า', cell: (r) => r.cust },
              {
                key: 'amt',
                header: 'ยอด',
                cell: (r) => <span className="tabular-nums">฿{r.amt.toLocaleString()}</span>,
                className: 'text-right',
                headerClassName: 'text-right',
              },
            ]}
            data={[
              { doc: 'BS20260427-001', cust: 'Acme Co.', amt: 12400 },
              { doc: 'BS20260427-002', cust: 'Shopee #abc', amt: 890 },
            ]}
            onRowClick={() => {}}
          />
        </Section>

        <Section title="Empty state">
          <EmptyState
            icon={Inbox}
            title="ยังไม่มีบิลในระบบ"
            description="นำเข้าจาก Lazada หรือ Shopee เพื่อเริ่มต้น"
            action={<Button size="sm">นำเข้าไฟล์</Button>}
          />
        </Section>

        <Section title="Loading skeleton">
          <div className="space-y-2">
            <RowSkeleton columns={4} />
            <RowSkeleton columns={4} />
            <RowSkeleton columns={4} />
          </div>
        </Section>

        <Section title="JSON viewer">
          <JsonViewer
            title="raw_data"
            data={{ source: 'shopee_shipped', order_id: '#abc', items: [{ name: 'INGU Vitamin C', qty: 1, price: 192 }] }}
            defaultOpen
          />
        </Section>

        <Section title="Dialog / Tooltip / Avatar / Breadcrumb">
          <div className="flex flex-wrap items-center gap-3">
            <Dialog>
              <DialogTrigger asChild>
                <Button variant="outline">เปิด Dialog</Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>หัวข้อ Dialog</DialogTitle>
                  <DialogDescription>
                    คำอธิบายสั้น ๆ เกี่ยวกับสิ่งที่กำลังจะเกิดขึ้น
                  </DialogDescription>
                </DialogHeader>
                <p className="text-sm">เนื้อหาภายใน Dialog</p>
              </DialogContent>
            </Dialog>

            <Button variant="outline" onClick={() => setConfirmOpen(true)}>
              ConfirmDialog (destructive)
            </Button>
            <ConfirmDialog
              open={confirmOpen}
              onOpenChange={setConfirmOpen}
              title="ลบรายการ?"
              description="การกระทำนี้ย้อนกลับไม่ได้"
              variant="destructive"
              confirmLabel="ลบ"
              onConfirm={() => new Promise((r) => setTimeout(r, 600))}
            />

            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" aria-label="search">
                  <Search className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>ค้นหา (⌘K)</TooltipContent>
            </Tooltip>

            <Avatar>
              <AvatarFallback>JC</AvatarFallback>
            </Avatar>

            <Breadcrumb>
              <BreadcrumbList>
                <BreadcrumbItem>
                  <BreadcrumbLink href="/">หน้าหลัก</BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbLink href="/bills">บิลทั้งหมด</BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbPage>BF-2026-001</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
          </div>
        </Section>

        <Section title="Skeleton primitive">
          <div className="space-y-2">
            <Skeleton className="h-8 w-1/2" />
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-4 w-2/3" />
          </div>
        </Section>

        <Section title="Typography (Inter + Noto Sans Thai)">
          <div className="space-y-2">
            <h1 className="text-3xl font-bold">หัวข้อใหญ่ — The quick brown fox</h1>
            <h2 className="text-xl font-semibold">หัวข้อรอง — Heading two</h2>
            <p className="text-base">
              ภาษาไทย: ระบบ Nexflow ช่วยให้พนักงานลดเวลาคีย์บิลจากวันละหลายร้อยใบลงเหลือเกือบศูนย์
              โดยใช้ AI extract ข้อมูลจากหลายช่องทาง — English: keep typography legible across both scripts.
            </p>
            <p className="font-mono text-sm">CON-01000 · NX-INV-20260427-abc12345</p>
          </div>
        </Section>

        <p className="pt-4 text-center text-xs text-muted-foreground">
          /dev/showcase — เฉพาะ dev mode (ลบใน Phase 4 ก่อน production release)
        </p>
      </div>
    </TooltipProvider>
  )
}
