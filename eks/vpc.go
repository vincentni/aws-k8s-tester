package eks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	awscfn "github.com/aws/aws-k8s-tester/pkg/aws/cloudformation"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"go.uber.org/zap"
)

// TemplateVPCPublic is the CloudFormation template for EKS VPC.
const TemplateVPCPublic = `
---
AWSTemplateFormatVersion: '2010-09-09'
Description: 'Amazon EKS VPC'

Parameters:

  VPCCIDR:
    Description: IP range (CIDR notation) for VPC, must be a valid private (RFC 1918) CIDR range (from 192.168.0.0 to 192.168.255.255)
    Type: String
    Default: 192.168.0.0/16
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PrivateSubnetCIDR1:
    Description: CIDR block for subnet 1 within the VPC (from 192.168.64.0 to 192.168.127.255)
    Type: String
    Default: 192.168.64.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PrivateSubnetCIDR2:
    Description: CIDR block for subnet 2 within the VPC (from 192.168.128.0 to 192.168.191.255)
    Type: String
    Default: 192.168.128.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PrivateSubnetCIDR3:
    Description: CIDR block for subnet 3 within the VPC (from 192.168.192.0 to 192.168.255.255)
    Type: String
    Default: 192.168.192.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

Conditions:

  Has2Azs:
    Fn::Or:
      - Fn::Equals:
        - {Ref: 'AWS::Region'}
        - ap-south-1
      - Fn::Equals:
        - {Ref: 'AWS::Region'}
        - ap-northeast-2
      - Fn::Equals:
        - {Ref: 'AWS::Region'}
        - ca-central-1
      - Fn::Equals:
        - {Ref: 'AWS::Region'}
        - cn-north-1
      - Fn::Equals:
        - {Ref: 'AWS::Region'}
        - sa-east-1
      - Fn::Equals:
        - {Ref: 'AWS::Region'}
        - us-west-1

  HasMoreThan2Azs:
    Fn::Not:
      - Condition: Has2Azs

Resources:

  InternetGateway:
    Type: AWS::EC2::InternetGateway
    Properties:
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-InternetGateway'

  VPC:
    Type: AWS::EC2::VPC
    Properties:
      CidrBlock: !Ref VPCCIDR
      EnableDnsSupport: true
      EnableDnsHostnames: true
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-VPC'

  VPCGatewayAttachment:
    Type: AWS::EC2::VPCGatewayAttachment
    DependsOn:
    - VPC
    Properties:
      InternetGatewayId: !Ref InternetGateway
      VpcId: !Ref VPC

  PublicRouteTable:
    Type: AWS::EC2::RouteTable
    DependsOn:
    - VPC
    Properties:
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicRouteTable'
      - Key: Network
        Value: Public

  PublicRoute:
    Type: AWS::EC2::Route
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      RouteTableId: !Ref PublicRouteTable
      DestinationCidrBlock: 0.0.0.0/0
      GatewayId: !Ref InternetGateway

  PublicSubnet1:
    Type: AWS::EC2::Subnet
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Public Subnet 1, https://docs.aws.amazon.com/eks/latest/userguide/alb-ingress.html
    Properties:
      AvailabilityZone: !Select [ 0, !GetAZs ]
      CidrBlock: !Ref PublicSubnetCIDR1
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicSubnet1'
      - Key: Network
        Value: Public
      - Key: kubernetes.io/role/elb
        Value: 1
      - Key: kubernetes.io/role/internal-elb
        Value: 1

  PublicSubnet2:
    Type: AWS::EC2::Subnet
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Public Subnet 2, https://docs.aws.amazon.com/eks/latest/userguide/alb-ingress.html
    Properties:
      AvailabilityZone: !Select [ 1, !GetAZs ]
      CidrBlock: !Ref PublicSubnetCIDR2
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicSubnet2'
      - Key: Network
        Value: Public
      - Key: kubernetes.io/role/elb
        Value: 1
      - Key: kubernetes.io/role/internal-elb
        Value: 1

  PublicSubnet3:
    Type: AWS::EC2::Subnet
    Condition: HasMoreThan2Azs
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Private Subnet 3, https://docs.aws.amazon.com/eks/latest/userguide/alb-ingress.html
    Properties:
      AvailabilityZone:
        Fn::Select:
        - '2'
        - Fn::GetAZs:
            Ref: AWS::Region
      CidrBlock: !Ref PublicSubnetCIDR3
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicSubnet3'
      - Key: Network
        Value: Public
      - Key: kubernetes.io/role/elb
        Value: 1
      - Key: kubernetes.io/role/internal-elb
        Value: 1

  PublicSubnet1RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet1
    Properties:
      SubnetId: !Ref PublicSubnet1
      RouteTableId: !Ref PublicRouteTable

  PublicSubnet2RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet2
    Properties:
      SubnetId: !Ref PublicSubnet2
      RouteTableId: !Ref PublicRouteTable

  PublicSubnet3RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    Condition: HasMoreThan2Azs
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet3
    Properties:
      SubnetId: !Ref PublicSubnet3
      RouteTableId: !Ref PublicRouteTable

  ControlPlaneSecurityGroup:
    Type: AWS::EC2::SecurityGroup
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      GroupDescription: Cluster communication with worker nodes
      VpcId: !Ref VPC

Outputs:

  VPCID:
    Description: VPC ID
    Value: !Ref VPC

  PublicSubnetIDs:
    Description: All private subnet IDs in the VPC
    Value:
      Fn::If:
      - HasMoreThan2Azs
      - !Join [ ",", [ !Ref PublicSubnet1, !Ref PublicSubnet2, !Ref PublicSubnet3 ] ]
      - !Join [ ",", [ !Ref PublicSubnet1, !Ref PublicSubnet2 ] ]

  ControlPlaneSecurityGroupID:
    Description: Security group ID for the cluster control plane communication with worker nodes
    Value: !Ref ControlPlaneSecurityGroup

`

// TemplateVPCPublicPrivate is the CloudFormation template for EKS VPC.
const TemplateVPCPublicPrivate = `
---
AWSTemplateFormatVersion: '2010-09-09'
Description: 'Amazon EKS VPC with public and private subnets'

Parameters:

  VPCCIDR:
    Description: IP range (CIDR notation) for VPC, must be a valid (RFC 1918) CIDR range (from 192.168.0.0 to 192.168.255.255)
    Type: String
    Default: 192.168.0.0/16
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PublicSubnetCIDR1:
    Description: CIDR block for public subnet 1 within the VPC (from 192.168.0.0 to 192.168.63.255)
    Type: String
    Default: 192.168.0.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PublicSubnetCIDR2:
    Description: CIDR block for public subnet 2 within the VPC (from 192.168.64.0 to 192.168.127.255)
    Type: String
    Default: 192.168.64.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PrivateSubnetCIDR1:
    Description: CIDR block for private subnet 1 within the VPC (from 192.168.128.0 to 192.168.191.255)
    Type: String
    Default: 192.168.128.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

  PrivateSubnetCIDR2:
    Description: CIDR block for private subnet 2 within the VPC (from 192.168.192.0 to 192.168.255.255)
    Type: String
    Default: 192.168.192.0/18
    AllowedPattern: '((\d{1,3})\.){3}\d{1,3}/\d{1,2}'

Resources:

  InternetGateway:
    Type: AWS::EC2::InternetGateway
    Properties:
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-InternetGateway'

  VPC:
    Type: AWS::EC2::VPC
    Properties:
      CidrBlock: !Ref VPCCIDR
      EnableDnsSupport: true
      EnableDnsHostnames: true
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-VPC'

  VPCGatewayAttachment:
    Type: AWS::EC2::VPCGatewayAttachment
    DependsOn:
    - VPC
    - InternetGateway
    Properties:
      InternetGatewayId: !Ref InternetGateway
      VpcId: !Ref VPC

  PublicSubnet1:
    Type: AWS::EC2::Subnet
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Public Subnet 1, https://docs.aws.amazon.com/eks/latest/userguide/alb-ingress.html
    Properties:
      AvailabilityZone: !Select [ 0, !GetAZs ]
      CidrBlock: !Ref PublicSubnetCIDR1
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicSubnet1'
      - Key: Network
        Value: Public
      - Key: kubernetes.io/role/elb
        Value: 1
      - Key: kubernetes.io/role/internal-elb
        Value: 1

  PublicSubnet2:
    Type: AWS::EC2::Subnet
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Public Subnet 2, https://docs.aws.amazon.com/eks/latest/userguide/alb-ingress.html
    Properties:
      AvailabilityZone: !Select [ 1, !GetAZs ]
      CidrBlock: !Ref PublicSubnetCIDR2
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicSubnet2'
      - Key: Network
        Value: Public
      - Key: kubernetes.io/role/elb
        Value: 1
      - Key: kubernetes.io/role/internal-elb
        Value: 1

  PublicRouteTable:
    Type: AWS::EC2::RouteTable
    DependsOn:
    - VPC
    Properties:
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PublicRouteTable'
      - Key: Network
        Value: Public

  PublicRoute:
    Type: AWS::EC2::Route
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      RouteTableId: !Ref PublicRouteTable
      DestinationCidrBlock: 0.0.0.0/0
      GatewayId: !Ref InternetGateway

  PublicSubnet1RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet1
    Properties:
      SubnetId: !Ref PublicSubnet1
      RouteTableId: !Ref PublicRouteTable

  PublicSubnet2RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet2
    Properties:
      SubnetId: !Ref PublicSubnet2
      RouteTableId: !Ref PublicRouteTable

  PrivateSubnet1:
    Type: AWS::EC2::Subnet
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Private Subnet 1
    Properties:
      AvailabilityZone: !Select [ 0, !GetAZs ]
      CidrBlock: !Ref PrivateSubnetCIDR1
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PrivateSubnet1'
      - Key: Network
        Value: Private

  PrivateSubnet2:
    Type: AWS::EC2::Subnet
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Metadata:
      Comment: Private Subnet 2
    Properties:
      AvailabilityZone: !Select [ 1, !GetAZs ]
      CidrBlock: !Ref PrivateSubnetCIDR2
      MapPublicIpOnLaunch: false
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PrivateSubnet2'
      - Key: Network
        Value: Private

  PrivateRouteTable1:
    Type: AWS::EC2::RouteTable
    Properties:
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PrivateRouteTable1'
      - Key: Network
        Value: Private1

  PrivateRouteTable2:
    Type: AWS::EC2::RouteTable
    Properties:
      VpcId: !Ref VPC
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-PrivateRouteTable2'
      - Key: Network
        Value: Private2

  NATGatewayEIP1:
    Type: AWS::EC2::EIP
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      Domain: vpc

  NATGatewayEIP2:
    Type: AWS::EC2::EIP
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      Domain: vpc

  NATGateway1:
    Type: AWS::EC2::NatGateway
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet1
    - NATGatewayEIP1
    Properties:
      AllocationId: !GetAtt 'NATGatewayEIP1.AllocationId'
      SubnetId: !Ref PublicSubnet1
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-NATGateway1'

  NATGateway2:
    Type: AWS::EC2::NatGateway
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PublicSubnet2
    - NATGatewayEIP2
    Properties:
      AllocationId: !GetAtt 'NATGatewayEIP2.AllocationId'
      SubnetId: !Ref PublicSubnet2
      Tags:
      - Key: Name
        Value: !Sub '${AWS::StackName}-NATGateway2'

  PrivateRoute1:
    Type: AWS::EC2::Route
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      RouteTableId: !Ref PrivateRouteTable1
      DestinationCidrBlock: 0.0.0.0/0
      NatGatewayId: !Ref NATGateway1

  PrivateRoute2:
    Type: AWS::EC2::Route
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      RouteTableId: !Ref PrivateRouteTable2
      DestinationCidrBlock: 0.0.0.0/0
      NatGatewayId: !Ref NATGateway2

  PrivateSubnet1RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PrivateSubnet1
    Properties:
      SubnetId: !Ref PrivateSubnet1
      RouteTableId: !Ref PrivateRouteTable1

  PrivateSubnet2RouteTableAssociation:
    Type: AWS::EC2::SubnetRouteTableAssociation
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    - PrivateSubnet2
    Properties:
      SubnetId: !Ref PrivateSubnet2
      RouteTableId: !Ref PrivateRouteTable2

  ControlPlaneSecurityGroup:
    Type: AWS::EC2::SecurityGroup
    DependsOn:
    - VPC
    - VPCGatewayAttachment
    Properties:
      GroupDescription: Cluster communication with worker nodes
      VpcId: !Ref VPC

Outputs:

  VPCID:
    Description: VPC ID
    Value: !Ref VPC

  PublicSubnetIDs:
    Description: All public subnet IDs in the VPC
    Value: !Join [ ",", [ !Ref PublicSubnet1, !Ref PublicSubnet2 ] ]

  PrivateSubnetIDs:
    Description: All private subnet IDs in the VPC
    Value: !Join [ ",", [ !Ref PrivateSubnet1, !Ref PrivateSubnet2 ] ]

  ControlPlaneSecurityGroupID:
    Description: Security group ID for the cluster control plane communication with worker nodes
    Value: !Ref ControlPlaneSecurityGroup

`

func (ts *Tester) createVPC() error {
	if ts.cfg.Status.VPCCFNStackID != "" ||
		(ts.cfg.Parameters.VPCID != "" &&
			len(ts.cfg.Status.PrivateSubnetIDs) > 0 &&
			ts.cfg.Status.ControlPlaneSecurityGroupID != "") {
		ts.lg.Info("non-empty VPC given; no need to create a new one")
		return nil
	}

	if ts.cfg.Parameters.VPCID != "" {
		ts.lg.Info("querying subnet IDs", zap.String("vpc-id", ts.cfg.Parameters.VPCID))
		sresp, err := ts.ec2API.DescribeSubnets(&ec2.DescribeSubnetsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: aws.StringSlice([]string{ts.cfg.Parameters.VPCID}),
				},
			},
		})
		if err != nil {
			ts.lg.Warn("failed to subnets", zap.Error(err))
			return err
		}

		ts.cfg.Status.PublicSubnetIDs = make([]string, 0, len(sresp.Subnets))
		ts.cfg.Status.PrivateSubnetIDs = make([]string, 0, len(sresp.Subnets))
		for _, sv := range sresp.Subnets {
			id := aws.StringValue(sv.SubnetId)
			networkTag := ""
			for _, tg := range sv.Tags {
				switch aws.StringValue(tg.Key) {
				case "Network":
					networkTag = aws.StringValue(tg.Value)
				}
				if networkTag != "" {
					break
				}
			}
			ts.lg.Info("found subnet",
				zap.String("id", id),
				zap.String("az", aws.StringValue(sv.AvailabilityZone)),
				zap.String("network-tag", networkTag),
			)
			switch networkTag {
			case "Public":
				ts.cfg.Status.PublicSubnetIDs = append(ts.cfg.Status.PublicSubnetIDs, id)
			case "Private":
				ts.cfg.Status.PrivateSubnetIDs = append(ts.cfg.Status.PrivateSubnetIDs, id)
			default:
				return fmt.Errorf("'Network' tag not found in subnet %q", id)
			}
		}
		if len(ts.cfg.Status.PrivateSubnetIDs) == 0 {
			return fmt.Errorf("no subnet found for VPC ID %q", ts.cfg.Parameters.VPCID)
		}

		ts.lg.Info("querying security IDs", zap.String("vpc-id", ts.cfg.Parameters.VPCID))
		gresp, err := ts.ec2API.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: aws.StringSlice([]string{ts.cfg.Parameters.VPCID}),
				},
			},
		})
		if err != nil {
			ts.lg.Warn("failed to security groups", zap.Error(err))
			return err
		}
		for _, sg := range gresp.SecurityGroups {
			id, name := aws.StringValue(sg.GroupId), aws.StringValue(sg.GroupName)
			ts.lg.Info("found security group", zap.String("id", id), zap.String("name", name))
			if name != "default" {
				ts.cfg.Status.ControlPlaneSecurityGroupID = id
			}
		}
		if ts.cfg.Status.ControlPlaneSecurityGroupID == "" {
			return fmt.Errorf("no security group found for VPC ID %q", ts.cfg.Parameters.VPCID)
		}

		ts.lg.Info("non-empty VPC given; no need to create a new one")
		ts.cfg.Status.VPCID = ts.cfg.Parameters.VPCID
		return ts.cfg.Sync()
	}

	templateBody := TemplateVPCPublic
	if ts.cfg.AddOnFargate.Enable {
		// e.g. An error occurred (InvalidParameterException) when calling the CreateFargateProfile operation: Subnet subnet-123 provided in Fargate Profile is not a private subnet
		templateBody = TemplateVPCPublicPrivate
	}

	// VPC attributes are empty, create a new VPC
	// otherwise, use the existing one
	ts.lg.Info("creating a new VPC")
	stackInput := &cloudformation.CreateStackInput{
		StackName:    aws.String(ts.cfg.Name + "-vpc"),
		Capabilities: aws.StringSlice([]string{"CAPABILITY_IAM"}),
		OnFailure:    aws.String(cloudformation.OnFailureDelete),
		TemplateBody: aws.String(templateBody),
		Tags: awscfn.NewTags(map[string]string{
			"Kind": "aws-k8s-tester",
			"Name": ts.cfg.Name,
		}),
		Parameters: []*cloudformation.Parameter{},
	}
	if ts.cfg.Parameters.VPCCIDR != "" {
		stackInput.Parameters = append(stackInput.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String("VPCCIDR"),
			ParameterValue: aws.String(ts.cfg.Parameters.VPCCIDR),
		})
	}
	if ts.cfg.Parameters.PublicSubnetCIDR1 != "" {
		stackInput.Parameters = append(stackInput.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String("PublicSubnetCIDR1"),
			ParameterValue: aws.String(ts.cfg.Parameters.PublicSubnetCIDR1),
		})
	}
	if ts.cfg.Parameters.PublicSubnetCIDR2 != "" {
		stackInput.Parameters = append(stackInput.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String("PublicSubnetCIDR2"),
			ParameterValue: aws.String(ts.cfg.Parameters.PublicSubnetCIDR2),
		})
	}
	if ts.cfg.Parameters.PublicSubnetCIDR3 != "" {
		stackInput.Parameters = append(stackInput.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String("PublicSubnetCIDR3"),
			ParameterValue: aws.String(ts.cfg.Parameters.PublicSubnetCIDR3),
		})
	}
	if ts.cfg.Parameters.PrivateSubnetCIDR1 != "" {
		stackInput.Parameters = append(stackInput.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String("PrivateSubnetCIDR1"),
			ParameterValue: aws.String(ts.cfg.Parameters.PrivateSubnetCIDR1),
		})
	}
	if ts.cfg.Parameters.PrivateSubnetCIDR2 != "" {
		stackInput.Parameters = append(stackInput.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String("PrivateSubnetCIDR2"),
			ParameterValue: aws.String(ts.cfg.Parameters.PrivateSubnetCIDR2),
		})
	}
	stackOutput, err := ts.cfnAPI.CreateStack(stackInput)
	if err != nil {
		return err
	}
	ts.cfg.Status.VPCCFNStackID = aws.StringValue(stackOutput.StackId)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	ch := awscfn.Poll(
		ctx,
		ts.stopCreationCh,
		ts.interruptSig,
		ts.lg,
		ts.cfnAPI,
		ts.cfg.Status.VPCCFNStackID,
		cloudformation.ResourceStatusCreateComplete,
		time.Minute+30*time.Second,
		20*time.Second,
	)
	var st awscfn.StackStatus
	for st = range ch {
		select {
		case <-ts.stopCreationCh:
			cancel()
			return errors.New("aborted")
		default:
		}
		if st.Error != nil {
			cancel()
			ts.cfg.Status.ClusterStatus = fmt.Sprintf("failed to create VPC (%v)", st.Error)
			ts.cfg.Sync()
			ts.lg.Warn("polling errror", zap.Error(st.Error))
		}
	}
	cancel()
	if st.Error != nil {
		return st.Error
	}
	// update status after creating a new VPC
	for _, o := range st.Stack.Outputs {
		switch k := aws.StringValue(o.OutputKey); k {
		case "VPCID":
			ts.cfg.Status.VPCID = aws.StringValue(o.OutputValue)
		case "PublicSubnetIDs":
			ts.cfg.Status.PublicSubnetIDs = strings.Split(aws.StringValue(o.OutputValue), ",")
		case "PrivateSubnetIDs":
			ts.cfg.Status.PrivateSubnetIDs = strings.Split(aws.StringValue(o.OutputValue), ",")
		case "ControlPlaneSecurityGroupID":
			ts.cfg.Status.ControlPlaneSecurityGroupID = aws.StringValue(o.OutputValue)
		default:
			return fmt.Errorf("unexpected OutputKey %q from %q", k, ts.cfg.Status.VPCCFNStackID)
		}
	}
	ts.lg.Info("created a VPC",
		zap.String("vpc-cfn-stack-id", ts.cfg.Status.VPCCFNStackID),
		zap.String("vpc-id", ts.cfg.Status.VPCID),
		zap.Strings("public-subnet-ids", ts.cfg.Status.PublicSubnetIDs),
		zap.Strings("private-subnet-ids", ts.cfg.Status.PrivateSubnetIDs),
		zap.String("control-plane-security-group-id", ts.cfg.Status.ControlPlaneSecurityGroupID),
	)
	return ts.cfg.Sync()
}

func (ts *Tester) deleteVPC() error {
	if ts.cfg.Parameters.VPCID != "" {
		ts.lg.Info("non-empty VPC given for reuse; do not delete VPC")
		return nil
	}
	if ts.cfg.Status.VPCCFNStackID == "" {
		ts.lg.Info("empty VPC CFN stack ID; no need to delete VPC")
		return nil
	}

	now := time.Now()

	ts.lg.Info("deleting VPC CFN stack", zap.String("vpc-cfn-stack-id", ts.cfg.Status.VPCCFNStackID))
	_, err := ts.cfnAPI.DeleteStack(&cloudformation.DeleteStackInput{
		StackName: aws.String(ts.cfg.Status.VPCCFNStackID),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	ch := awscfn.Poll(
		ctx,
		make(chan struct{}),  // do not exit on stop
		make(chan os.Signal), // do not exit on stop
		ts.lg,
		ts.cfnAPI,
		ts.cfg.Status.VPCCFNStackID,
		cloudformation.ResourceStatusDeleteComplete,
		time.Minute+30*time.Second,
		20*time.Second,
	)

	deletedResources := make(map[string]struct{})
	var st awscfn.StackStatus
	for st = range ch {
		if st.Error != nil {
			cancel()
			ts.cfg.Status.ClusterStatus = fmt.Sprintf("failed to delete VPC (%v)", st.Error)
			ts.cfg.Sync()
			ts.lg.Warn("polling errror", zap.Error(st.Error))
		}

		if time.Now().Sub(now) > 3*time.Minute {
			ts.lg.Warn("deleting VPC for longer than 3 minutes; initiating force deletion",
				zap.String("vpc-id", ts.cfg.Status.VPCID),
			)
			for _, subnetID := range ts.cfg.Status.PrivateSubnetIDs {
				if _, ok := deletedResources[subnetID]; ok {
					continue
				}
				_, serr := ts.ec2API.DeleteSubnet(&ec2.DeleteSubnetInput{
					SubnetId: aws.String(subnetID),
				})
				ts.lg.Warn("tried force-delete subnet",
					zap.String("subnet-id", subnetID),
					zap.Error(serr),
				)
				if serr != nil && strings.Contains(serr.Error(), " does not exist") {
					deletedResources[subnetID] = struct{}{}
				}
			}
			if _, ok := deletedResources[ts.cfg.Status.VPCID]; ok {
				continue
			}

			// TODO: deleting VPC doesn't work because of dependencies...
			// e.g. DependencyViolation: The vpc 'vpc-0127f6d18bd98836a' has dependencies and cannot be deleted
			ts.lg.Warn("cleaning VPC dependencies", zap.String("vpc-id", ts.cfg.Status.VPCID))

			// find all ENIs for VPC
			enis := make([]*ec2.NetworkInterface, 0)
			if err := ts.ec2API.DescribeNetworkInterfacesPages(
				&ec2.DescribeNetworkInterfacesInput{
					Filters: []*ec2.Filter{
						{
							Name:   aws.String("vpc-id"),
							Values: aws.StringSlice([]string{ts.cfg.Status.VPCID}),
						},
					},
				},
				func(out *ec2.DescribeNetworkInterfacesOutput, lastPage bool) bool {
					for _, eni := range out.NetworkInterfaces {
						enis = append(enis, eni)
						ts.lg.Info("found ENI", zap.String("eni", aws.StringValue(eni.NetworkInterfaceId)))
					}
					return true
				},
			); err != nil {
				ts.lg.Warn("failed to describe ENIs", zap.Error(err))
				continue
			}

			// detacth and delete ENIs
			for _, eni := range enis {
				eniID := aws.StringValue(eni.NetworkInterfaceId)

				ts.lg.Warn("detaching ENI", zap.String("eni", eniID))
				out, err := ts.ec2API.DescribeNetworkInterfaces(
					&ec2.DescribeNetworkInterfacesInput{
						NetworkInterfaceIds: []*string{eni.NetworkInterfaceId},
					},
				)
				if err != nil {
					ts.lg.Warn("failed to describe ENI", zap.Error(err))
					continue
				}
				if len(out.NetworkInterfaces) != 1 {
					ts.lg.Warn("expected 1 ENI", zap.String("eni", eniID), zap.Int("enis", len(out.NetworkInterfaces)))
					continue
				}
				if out.NetworkInterfaces[0].Attachment == nil {
					ts.lg.Warn("no attachment found for ENI", zap.String("eni", eniID))
				} else {
					for i := 0; i < 5; i++ {
						time.Sleep(5 * time.Second)
						_, err = ts.ec2API.DetachNetworkInterface(&ec2.DetachNetworkInterfaceInput{
							AttachmentId: out.NetworkInterfaces[0].Attachment.AttachmentId,
							Force:        aws.Bool(true),
						})
						if err == nil {
							ts.lg.Info("successfully detached ENI", zap.String("eni", eniID))
							break
						}
						ts.lg.Warn("failed to detach ENI", zap.String("eni", eniID), zap.Error(err))
					}
				}

				for i := 0; i < 5; i++ {
					time.Sleep(10 * time.Second) // it may take awhile for delete to success upon detach
					ts.lg.Info("deleting ENI", zap.String("eni", eniID))
					_, err = ts.ec2API.DeleteNetworkInterface(&ec2.DeleteNetworkInterfaceInput{
						NetworkInterfaceId: eni.NetworkInterfaceId,
					})
					if err == nil {
						ts.lg.Info("successfully deleted ENI", zap.String("eni", eniID))
						break
					}
					ts.lg.Warn("failed to delete ENI", zap.String("eni", eniID), zap.Error(err))
				}

				// confirm ENI deletion
				retryStart := time.Now()
				for time.Now().Sub(retryStart) < 5*time.Minute {
					time.Sleep(5 * time.Second)
					_, err = ts.ec2API.DescribeNetworkInterfaces(&ec2.DescribeNetworkInterfacesInput{
						NetworkInterfaceIds: []*string{eni.NetworkInterfaceId},
					})
					if err == nil {
						ts.lg.Warn("ENI still exists", zap.String("eni", eniID))
						continue
					}
					if awsErr, ok := err.(awserr.Error); ok {
						if strings.Contains(awsErr.Code(), "InvalidNetworkInterfaceID.NotFound") {
							ts.lg.Info("confirmed ENI deletion", zap.String("eni", eniID))
							break
						}
					}
					ts.lg.Warn("ENI still exists", zap.String("eni", eniID), zap.Error(err))
				}
			}

			// clean up security groups for VPC
			sout, err := ts.ec2API.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
				Filters: []*ec2.Filter{
					{
						Name:   aws.String("vpc-id"),
						Values: []*string{aws.String(ts.cfg.Status.VPCID)},
					},
				},
			})
			if err != nil {
				ts.lg.Warn("failed to describe security groups", zap.Error(err))
				continue
			}
			for _, sg := range sout.SecurityGroups {
				sgID, sgGroupName := aws.StringValue(sg.GroupId), aws.StringValue(sg.GroupName)
				ts.lg.Info("cleaning security group",
					zap.String("security-group-id", sgID),
					zap.String("security-group-name", sgGroupName),
				)

				for _, ipPerm := range sg.IpPermissions {
					ts.lg.Info("revoking ingress", zap.String("ip-perm", ipPerm.String()))
					_, err = ts.ec2API.RevokeSecurityGroupIngress(&ec2.RevokeSecurityGroupIngressInput{
						GroupId:       aws.String(sgID),
						IpPermissions: []*ec2.IpPermission{ipPerm},
					})
					ts.lg.Info("tried to revoke ingress", zap.Error(err))

					if len(ipPerm.UserIdGroupPairs) != 1 {
						continue
					}
					sgIDEgress := aws.StringValue(ipPerm.UserIdGroupPairs[0].GroupId)
					sgNameEgress := aws.StringValue(ipPerm.UserIdGroupPairs[0].GroupName)
					sgEgress, err := ts.ec2API.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
						GroupIds: aws.StringSlice([]string{sgIDEgress}),
					})
					if err != nil {
						ts.lg.Warn("failed to describe egress security group", zap.Error(err))
						continue
					}
					if len(sgEgress.SecurityGroups) != 1 {
						ts.lg.Warn("expected only 1 security group",
							zap.String("egress-security-group-id", sgIDEgress),
							zap.String("egress-security-group-name", sgNameEgress),
							zap.Int("total", len(sgEgress.SecurityGroups)),
						)
						continue
					}
					for _, ipPermEg := range sgEgress.SecurityGroups[0].IpPermissionsEgress {
						ts.lg.Info("revoking egress", zap.String("ip-perm", ipPermEg.String()))
						_, err = ts.ec2API.RevokeSecurityGroupEgress(&ec2.RevokeSecurityGroupEgressInput{
							GroupId:       aws.String(sgIDEgress),
							IpPermissions: []*ec2.IpPermission{ipPermEg},
						})
						ts.lg.Info("tried to revoke egress", zap.Error(err))
					}
				}

				for _, ipPerm := range sg.IpPermissionsEgress {
					ts.lg.Info("revoking egress",
						zap.String("security-group-id", sgID),
						zap.String("ip-perm", ipPerm.String()),
					)
					_, err = ts.ec2API.RevokeSecurityGroupEgress(&ec2.RevokeSecurityGroupEgressInput{
						GroupId:       aws.String(sgID),
						IpPermissions: []*ec2.IpPermission{ipPerm},
					})
					if err != nil {
						if ev, ok := err.(awserr.Error); ok && ev.Code() == "InvalidPermission.NotFound" {
							ts.lg.Warn("ip permission does not exist", zap.Error(err))
						} else {
							ts.lg.Warn("failed to revoke egress", zap.Error(err))
						}
					} else {
						ts.lg.Info("revoked egress",
							zap.String("security-group-id", sgID),
							zap.Error(err),
						)
					}

					if len(ipPerm.UserIdGroupPairs) != 1 {
						continue
					}
					sgIDIngress := aws.StringValue(ipPerm.UserIdGroupPairs[0].GroupId)
					sgNameIngress := aws.StringValue(ipPerm.UserIdGroupPairs[0].GroupName)
					sgIngress, err := ts.ec2API.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
						GroupIds: aws.StringSlice([]string{sgIDIngress}),
					})
					if err != nil {
						ts.lg.Warn("failed to describe egress security group", zap.Error(err))
						continue
					}
					if len(sgIngress.SecurityGroups) != 1 {
						ts.lg.Warn("expected only 1 security group",
							zap.String("ingress-security-group-id", sgIDIngress),
							zap.String("ingress-security-group-name", sgNameIngress),
							zap.Int("total", len(sgIngress.SecurityGroups)),
						)
						continue
					}
					for _, ipPermEg := range sgIngress.SecurityGroups[0].IpPermissionsEgress {
						ts.lg.Info("revoking ingress", zap.String("ip-perm", ipPermEg.String()))
						_, err = ts.ec2API.RevokeSecurityGroupIngress(&ec2.RevokeSecurityGroupIngressInput{
							GroupId:       aws.String(sgIDIngress),
							IpPermissions: []*ec2.IpPermission{ipPermEg},
						})
						if err != nil {
							if ev, ok := err.(awserr.Error); ok && ev.Code() == "InvalidPermission.NotFound" {
								ts.lg.Warn("ip permission does not exist", zap.Error(err))
							} else {
								ts.lg.Warn("failed to revoke ingress", zap.Error(err))
							}
						} else {
							ts.lg.Info("revoked ingress",
								zap.String("security-group-id", sgID),
								zap.Error(err),
							)
						}
					}
				}

				ts.lg.Info("deleting security group",
					zap.String("security-group-id", sgID),
					zap.String("security-group-name", sgGroupName),
				)
				_, err = ts.ec2API.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
					GroupId: sg.GroupId,
				})
				if err != nil {
					ts.lg.Warn("failed to delete security group", zap.Error(err))
					continue
				}
				retryStart := time.Now()
				for time.Now().Sub(retryStart) < 5*time.Minute {
					_, err = ts.ec2API.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
						GroupIds: []*string{sg.GroupId},
					})
					if err != nil {
						if awsErr, ok := err.(awserr.Error); ok {
							if strings.Contains(awsErr.Code(), ".NotFound") {
								ts.lg.Info("successfully deleted security group",
									zap.String("security-group-id", sgID),
									zap.String("security-group-name", sgGroupName),
								)
								break
							}
						}
					}
					ts.lg.Warn("still deleting security group", zap.Error(err))
					time.Sleep(5 * time.Second)
				}
			}

			_, derr := ts.ec2API.DeleteVpc(&ec2.DeleteVpcInput{VpcId: aws.String(ts.cfg.Status.VPCID)})
			if derr != nil {
				ts.lg.Warn("failed to force-delete VPC",
					zap.String("vpc-id", ts.cfg.Status.VPCID),
					zap.Error(derr),
				)
			} else {
				ts.lg.Info("force-deleted VPC",
					zap.String("vpc-id", ts.cfg.Status.VPCID),
				)
			}
			if derr != nil && strings.Contains(derr.Error(), " does not exist") {
				deletedResources[ts.cfg.Status.VPCID] = struct{}{}
			}
		}
	}
	cancel()
	if st.Error != nil {
		return st.Error
	}
	ts.lg.Info("deleted a VPC",
		zap.String("vpc-cfn-stack-id", ts.cfg.Status.VPCCFNStackID),
		zap.String("vpc-id", ts.cfg.Status.VPCID),
	)
	return ts.cfg.Sync()
}
